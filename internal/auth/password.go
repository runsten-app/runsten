package auth

import (
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/crypto/argon2"
)

// Password and username rules. NIST SP 800-63B (revision 4) asks for at least 15
// characters for a password that is the only factor, no composition rule, and
// accepting long passphrases.
const (
	MinPasswordLen   = 15   // characters
	maxPasswordBytes = 1024 // bounds the request, not the hash cost
	maxUsernameLen   = 254  // an e-mail address fits, for a hosted instance
	saltLen          = 16
)

// HashParams is the cost of an argon2id hash (RFC 9106).
type HashParams struct {
	MemoryKiB  uint32
	Iterations uint32
	Threads    uint8
	KeyLen     uint32
}

// DefaultHashParams returns the minimum recommended by OWASP for argon2id: 19 MiB,
// 2 iterations, 1 thread. It fits a Raspberry Pi.
func DefaultHashParams() HashParams {
	return HashParams{MemoryKiB: 19 * 1024, Iterations: 2, Threads: 1, KeyLen: 32}
}

// NormalizeUsername returns the canonical form of a username (usernames are case
// insensitive), or an error if it is empty, too long, or contains spaces or control
// characters.
func NormalizeUsername(name string) (string, error) {
	name = strings.ToLower(strings.TrimSpace(name))
	switch {
	case name == "":
		return "", errors.New("the username is empty")
	case len(name) > maxUsernameLen:
		return "", fmt.Errorf("the username is longer than %d bytes", maxUsernameLen)
	case strings.IndexFunc(name, func(r rune) bool { return unicode.IsSpace(r) || !unicode.IsPrint(r) }) >= 0:
		return "", errors.New("the username contains spaces or control characters")
	}
	return name, nil
}

// ValidatePassword checks a new password.
func ValidatePassword(password string) error {
	switch {
	case !utf8.ValidString(password):
		return errors.New("the password is not valid UTF-8")
	case utf8.RuneCountInString(password) < MinPasswordLen:
		return fmt.Errorf("the password must have at least %d characters", MinPasswordLen)
	case len(password) > maxPasswordBytes:
		return fmt.Errorf("the password is longer than %d bytes", maxPasswordBytes)
	}
	return nil
}

// HashPassword returns the argon2id hash of password, with a random salt read from
// random, as a PHC string: $argon2id$v=19$m=19456,t=2,p=1$<salt>$<hash>.
func HashPassword(password string, p HashParams, random io.Reader) (string, error) {
	salt := make([]byte, saltLen)
	if _, err := io.ReadFull(random, salt); err != nil {
		return "", fmt.Errorf("salt: %w", err)
	}
	key := argon2.IDKey([]byte(password), salt, p.Iterations, p.MemoryKiB, p.Threads, p.KeyLen)
	b64 := base64.RawStdEncoding
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, p.MemoryKiB, p.Iterations, p.Threads, b64.EncodeToString(salt), b64.EncodeToString(key)), nil
}

// verifyPassword reports whether password matches the PHC string encoded, with the
// cost stored in it. The comparison runs in constant time.
func verifyPassword(encoded, password string) (bool, error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[0] != "" || parts[1] != "argon2id" {
		return false, errors.New("not an argon2id hash")
	}
	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil || version != argon2.Version {
		return false, fmt.Errorf("unsupported argon2 version %q", parts[2])
	}
	var p HashParams
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &p.MemoryKiB, &p.Iterations, &p.Threads); err != nil ||
		p.Iterations == 0 || p.Threads == 0 {
		return false, fmt.Errorf("invalid argon2 parameters %q", parts[3])
	}
	b64 := base64.RawStdEncoding
	salt, err := b64.DecodeString(parts[4])
	if err != nil {
		return false, fmt.Errorf("salt: %w", err)
	}
	want, err := b64.DecodeString(parts[5])
	if err != nil || len(want) == 0 {
		return false, errors.New("invalid argon2 key")
	}
	got := argon2.IDKey([]byte(password), salt, p.Iterations, p.MemoryKiB, p.Threads, uint32(len(want))) //nolint:gosec // len(want) comes from a stored hash
	return subtle.ConstantTimeCompare(got, want) == 1, nil
}
