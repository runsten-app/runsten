package archive

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// S3Config is the bucket the archives are kept in.
type S3Config struct {
	// Endpoint is the service's URL: https://s3.fr-par.scw.cloud, http://minio:9000.
	Endpoint string
	Bucket   string
	Region   string
	// AccessKey and SecretKey are the credentials, which may write the bucket's
	// exports/ and presign its reads.
	AccessKey, SecretKey string
}

// S3 keeps the archives in a bucket: exports/<account>/<export>.zip, uploaded in parts
// as they are written (a multipart upload), visible once complete. A download is sent
// to a presigned URL of the object: the bucket serves it, its ranges included.
//
// An upload that never ends (a worker killed) leaves its parts in the bucket until
// its lifecycle aborts it: the bucket needs a rule AbortIncompleteMultipartUpload.
type S3 struct {
	c      *minio.Client
	bucket string
}

const (
	// s3Prefix holds the archives in the bucket.
	s3Prefix = "exports/"
	// s3PartSize is the size of an upload's parts: what the upload holds in memory.
	s3PartSize = 16 << 20
	// s3LinkTTL is how long a presigned URL is valid: the time to start the download.
	s3LinkTTL = 15 * time.Minute
)

// NewS3 connects to the bucket (no request yet).
func NewS3(cfg S3Config) (*S3, error) {
	u, err := url.Parse(cfg.Endpoint)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || cfg.Bucket == "" {
		return nil, fmt.Errorf("archive bucket: an endpoint http(s)://host and a bucket expected, got %q, %q", cfg.Endpoint, cfg.Bucket)
	}
	c, err := minio.New(u.Host, &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.AccessKey, cfg.SecretKey, ""),
		Secure: u.Scheme == "https",
		Region: cfg.Region,
	})
	if err != nil {
		return nil, fmt.Errorf("archive bucket: %w", err)
	}
	return &S3{c: c, bucket: cfg.Bucket}, nil
}

func s3Key(accountID, exportID string) string { return s3Prefix + accountID + "/" + exportID + suffix }

// Create starts the export's archive, uploaded as it is written. Close completes the
// upload, unless ctx is over: the upload is then given up.
func (s *S3) Create(ctx context.Context, accountID, exportID string) (io.WriteCloser, error) {
	if err := checkIDs(accountID, exportID); err != nil {
		return nil, err
	}
	r, w := io.Pipe()
	upload, cancel := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() {
		_, err := s.c.PutObject(upload, s.bucket, s3Key(accountID, exportID), r, -1, minio.PutObjectOptions{
			ContentType: "application/zip", PartSize: s3PartSize,
		})
		_ = r.CloseWithError(err) // a write after a failed upload fails with it
		done <- err
	}()
	return &s3Writer{ctx: ctx, w: w, cancel: cancel, done: done}, nil
}

type s3Writer struct {
	ctx    context.Context
	w      *io.PipeWriter
	cancel context.CancelFunc
	done   chan error
}

func (w *s3Writer) Write(p []byte) (int, error) {
	n, err := w.w.Write(p)
	if err != nil {
		return n, fmt.Errorf("archive upload: %w", err)
	}
	return n, nil
}

func (w *s3Writer) Close() error {
	defer w.cancel()
	if err := w.ctx.Err(); err != nil {
		_ = w.w.CloseWithError(err)
		<-w.done
		return fmt.Errorf("archive upload: %w", err)
	}
	_ = w.w.Close() // the end of the stream: the upload completes
	if err := <-w.done; err != nil {
		return fmt.Errorf("archive upload: %w", err)
	}
	return nil
}

// Open returns a presigned URL of the export's archive, downloaded under filename;
// fs.ErrNotExist when there is none.
func (s *S3) Open(ctx context.Context, accountID, exportID, filename string) (io.ReadSeekCloser, string, error) {
	if err := checkIDs(accountID, exportID); err != nil {
		return nil, "", err
	}
	key := s3Key(accountID, exportID)
	if _, err := s.c.StatObject(ctx, s.bucket, key, minio.StatObjectOptions{}); err != nil {
		if minio.ToErrorResponse(err).StatusCode == http.StatusNotFound {
			return nil, "", fmt.Errorf("archive: %w", fs.ErrNotExist)
		}
		return nil, "", fmt.Errorf("archive bucket: %w", err)
	}
	params := url.Values{}
	params.Set("response-content-disposition", mime.FormatMediaType("attachment", map[string]string{"filename": filename}))
	params.Set("response-content-type", "application/zip")
	u, err := s.c.PresignedGetObject(ctx, s.bucket, key, s3LinkTTL, params)
	if err != nil {
		return nil, "", fmt.Errorf("archive bucket: %w", err)
	}
	return nil, u.String(), nil
}

// Remove removes the export's archive; none is no error.
func (s *S3) Remove(ctx context.Context, accountID, exportID string) error {
	if err := checkIDs(accountID, exportID); err != nil {
		return err
	}
	if err := s.c.RemoveObject(ctx, s.bucket, s3Key(accountID, exportID), minio.RemoveObjectOptions{}); err != nil {
		return fmt.Errorf("archive bucket: %w", err)
	}
	return nil
}

// List lists the exports that have an archive: their account by their id.
func (s *S3) List(ctx context.Context) (map[string]string, error) {
	out := map[string]string{}
	for o := range s.c.ListObjects(ctx, s.bucket, minio.ListObjectsOptions{Prefix: s3Prefix, Recursive: true}) {
		if o.Err != nil {
			return nil, fmt.Errorf("archive bucket: %w", o.Err)
		}
		account, name, ok := strings.Cut(strings.TrimPrefix(o.Key, s3Prefix), "/")
		id, found := strings.CutSuffix(name, suffix)
		if ok && found && ids.MatchString(account) && ids.MatchString(id) {
			out[id] = account
		}
	}
	return out, nil
}
