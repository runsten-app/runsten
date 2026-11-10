// Package archive keeps the accounts' export archives out of the database: in a
// directory of the machine (Dir, the default, a volume in Docker Compose) or in an S3
// bucket (S3, for an instance of several replicas, or that wants them elsewhere). An
// archive is named by its account and its export; it is written as a stream and
// becomes visible only once complete, read back by offset (a file) or through a
// presigned URL (a bucket).
package archive

import (
	"errors"
	"fmt"
	"regexp"
)

// ids are the accounts' and the exports' ids: UUIDs, nothing else makes a path or a key.
var ids = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// errID is returned for a name that is not a UUID.
var errID = errors.New("archive: not a UUID")

func checkIDs(accountID, exportID string) error {
	if !ids.MatchString(accountID) || !ids.MatchString(exportID) {
		return fmt.Errorf("%w: %q, %q", errID, accountID, exportID)
	}
	return nil
}

// suffix ends an archive's name.
const suffix = ".zip"
