package postgresql

import (
	"crypto/hmac"
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"regexp"

	"github.com/xdg-go/stringprep"
)

const (
	// PostgreSQL's own defaults (SCRAM_SHA_256_DEFAULT_ITERATIONS and
	// SCRAM_DEFAULT_SALT_LEN in src/include/common/scram-common.h).
	scramSHA256DefaultIterations = 4096
	scramSHA256SaltLength        = 16
)

var (
	// The two formats PostgreSQL stores as-is instead of hashing them, see
	// get_password_type() in src/backend/libpq/crypt.c.
	md5PasswordRegexp         = regexp.MustCompile(`^md5[0-9a-f]{32}$`)
	scramSHA256PasswordRegexp = regexp.MustCompile(`^SCRAM-SHA-256\$[0-9]+:[A-Za-z0-9+/]+={0,2}\$[A-Za-z0-9+/]+={0,2}:[A-Za-z0-9+/]+={0,2}$`)
)

// isHashedPassword returns true if the password is already in one of the
// hashed formats PostgreSQL stores as-is (MD5 or SCRAM-SHA-256).
func isHashedPassword(password string) bool {
	return md5PasswordRegexp.MatchString(password) || scramSHA256PasswordRegexp.MatchString(password)
}

// newScramSHA256Salt returns a random salt of the length PostgreSQL uses.
func newScramSHA256Salt() ([]byte, error) {
	salt := make([]byte, scramSHA256SaltLength)
	if _, err := rand.Read(salt); err != nil {
		return nil, fmt.Errorf("could not generate a salt: %w", err)
	}
	return salt, nil
}

// scramSHA256Password hashes a clear-text password into the SCRAM-SHA-256
// format PostgreSQL stores in pg_authid.rolpassword (RFC 5802 and RFC 7677):
//
//	SCRAM-SHA-256$<iterations>:<salt>$<StoredKey>:<ServerKey>
//
// It is what PostgreSQL itself computes when it receives a clear-text password
// with password_encryption = 'scram-sha-256' (and what psql's \password
// computes client-side), so a role can log in with the clear-text password.
//
// The key derivation is adapted from
// https://github.com/tailsdotcom/terraform-provider-scram (MPL-2.0).
func scramSHA256Password(password string, salt []byte, iterations int) (string, error) {
	// PostgreSQL normalizes the password with SASLprep and keeps the original
	// one if it cannot be normalized (e.g. it contains prohibited characters).
	if prepared, err := stringprep.SASLprep.Prepare(password); err == nil {
		password = prepared
	}

	saltedPassword, err := pbkdf2.Key(sha256.New, password, salt, iterations, sha256.Size)
	if err != nil {
		return "", fmt.Errorf("could not hash the password: %w", err)
	}

	clientKey := hmacSHA256(saltedPassword, "Client Key")
	storedKey := sha256.Sum256(clientKey)
	serverKey := hmacSHA256(saltedPassword, "Server Key")

	return fmt.Sprintf(
		"SCRAM-SHA-256$%d:%s$%s:%s",
		iterations,
		base64.StdEncoding.EncodeToString(salt),
		base64.StdEncoding.EncodeToString(storedKey[:]),
		base64.StdEncoding.EncodeToString(serverKey),
	), nil
}

func hmacSHA256(key []byte, message string) []byte {
	hasher := hmac.New(sha256.New, key)
	hasher.Write([]byte(message))
	return hasher.Sum(nil)
}
