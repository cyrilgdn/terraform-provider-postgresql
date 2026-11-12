package postgresql

import (
	"crypto/hmac"
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/xdg-go/stringprep"
	"golang.org/x/text/unicode/norm"
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
	md5PasswordRegexp = regexp.MustCompile(`^md5[0-9a-f]{32}$`)
	// SCRAM-SHA-256$<iterations>:<salt>$<StoredKey>:<ServerKey>, the keys being
	// the base64 of a SHA-256 hash.
	scramSHA256PasswordRegexp = regexp.MustCompile(`^SCRAM-SHA-256\$[0-9]{1,9}:([A-Za-z0-9+/=]+)\$[A-Za-z0-9+/]{43}=:[A-Za-z0-9+/]{43}=$`)
)

// isHashedPassword returns true if the password is in one of the hashed
// formats PostgreSQL stores as-is (MD5 or SCRAM-SHA-256), see
// parse_scram_secret() in src/backend/libpq/auth-scram.c. A string it does not
// recognize is hashed like any other password, so it errs on the safe side: the
// iterations are at most 9 digits, without the sign or the leading spaces
// PostgreSQL also accepts.
func isHashedPassword(password string) bool {
	if md5PasswordRegexp.MatchString(password) {
		return true
	}
	parts := scramSHA256PasswordRegexp.FindStringSubmatch(password)
	if parts == nil {
		return false
	}
	_, err := base64.StdEncoding.DecodeString(parts[1])
	return err == nil
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
	prepared, err := saslprepPassword(password)
	if err != nil {
		return "", err
	}
	saltedPassword, err := pbkdf2.Key(sha256.New, prepared, salt, iterations, sha256.Size)
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

// saslprep is SASLprep (RFC 4013) the way pg_saslprep() in
// src/common/saslprep.c applies it: U+200B is mapped to a space rather than to
// nothing, U+1806 is mapped to nothing (RFC 3454, table B.1), and the
// prohibited and bidirectional characters are checked before the normalization.
var saslprep = stringprep.Profile{
	Mappings:  append([]stringprep.Mapping{{0x200B: {' '}, 0x1806: {}}}, stringprep.SASLprep.Mappings...),
	Prohibits: stringprep.SASLprep.Prohibits,
	CheckBiDi: true,
}

var errLongCombiningSequence = errors.New("hash_password does not support a password with more than 30 combining characters in a row, as it cannot be normalized the way PostgreSQL does")

// saslprepPassword returns the password PostgreSQL actually hashes: the
// password prepared with SASLprep, or the password as it is when it cannot be
// prepared (e.g. it contains a prohibited character) or is mapped to nothing.
//
// The normalization needs golang.org/x/text v0.42.0 or later, which composes
// characters like PostgreSQL. After 30 characters in a row that combine with
// the one before them (combining marks, Hangul vowel and final jamo, counted
// after decomposition) norm.NFKC inserts U+034F and orders each part on its
// own, while PostgreSQL orders the whole run. The mapping removes U+034F, so
// one in the result was inserted and the password is refused.
func saslprepPassword(password string) (string, error) {
	mapped, err := saslprep.Prepare(password)
	if err != nil || mapped == "" {
		return password, nil
	}
	normalized := norm.NFKC.String(mapped)
	if strings.Contains(normalized, norm.GraphemeJoiner) {
		return "", errLongCombiningSequence
	}
	return normalized, nil
}
