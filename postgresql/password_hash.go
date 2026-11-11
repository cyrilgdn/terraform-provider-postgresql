package postgresql

import (
	"crypto/hmac"
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"regexp"
	"strconv"
	"unicode/utf8"

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
	// SCRAM-SHA-256$<iterations>:<salt>$<StoredKey>:<ServerKey>
	scramSHA256PasswordRegexp = regexp.MustCompile(`^SCRAM-SHA-256\$\s*([+-]?[0-9]+):([A-Za-z0-9+/=]+)\$([A-Za-z0-9+/=]+):([A-Za-z0-9+/=]+)$`)
)

// isHashedPassword returns true if the password is already in one of the
// hashed formats PostgreSQL stores as-is (MD5 or SCRAM-SHA-256).
//
// A string which only looks like a SCRAM-SHA-256 password is a clear-text
// password for PostgreSQL, see parse_scram_secret() in
// src/backend/libpq/auth-scram.c: the iterations have to be a number (as
// strtol() reads it), the salt has to be base64 and both keys have to be the
// base64 of a SHA-256 hash.
func isHashedPassword(password string) bool {
	if md5PasswordRegexp.MatchString(password) {
		return true
	}

	parts := scramSHA256PasswordRegexp.FindStringSubmatch(password)
	if parts == nil {
		return false
	}
	if _, err := strconv.ParseInt(parts[1], 10, 64); err != nil {
		return false
	}
	if _, err := base64.StdEncoding.DecodeString(parts[2]); err != nil {
		return false
	}
	for _, key := range parts[3:] {
		if decoded, err := base64.StdEncoding.DecodeString(key); err != nil || len(decoded) != sha256.Size {
			return false
		}
	}
	return true
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
	saltedPassword, err := pbkdf2.Key(sha256.New, saslprepPassword(password), salt, iterations, sha256.Size)
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

var (
	// The characters "commonly mapped to nothing" (RFC 3454, table B.1).
	saslprepMappedToNothing = stringprep.Set{
		{0x00AD, 0x00AD}, {0x034F, 0x034F}, {0x1806, 0x1806}, {0x180B, 0x180D},
		{0x200B, 0x200D}, {0x2060, 0x2060}, {0xFE00, 0xFE0F}, {0xFEFF, 0xFEFF},
	}

	// The characters a prepared password cannot contain: the prohibited ones
	// (RFC 4013, section 2.3) and the ones Unicode 3.2 does not assign (RFC
	// 3454, table A.1).
	saslprepProhibited = []stringprep.Set{
		stringprep.TableC2_1, stringprep.TableC2_2, stringprep.TableC3,
		stringprep.TableC4, stringprep.TableC5, stringprep.TableC6,
		stringprep.TableC7, stringprep.TableC8, stringprep.TableC9,
		stringprep.TableA1,
	}
)

// saslprepPassword returns the password PostgreSQL actually hashes: the
// password prepared with SASLprep (RFC 4013), or the password as it is when it
// cannot be prepared (e.g. it contains a prohibited character).
//
// This is a port of pg_saslprep() in src/common/saslprep.c, which the server
// and libpq both use, rather than a use of stringprep.SASLprep: PostgreSQL
// differs from it for some characters, and a password prepared in another way
// gives a hash the role cannot log in with. In particular PostgreSQL maps a
// non-ASCII space to a space before removing the characters mapped to nothing
// (U+200B is both), and it looks for the prohibited, unassigned and
// bidirectional characters before the normalization, not after it.
func saslprepPassword(password string) string {
	if isASCII(password) || !utf8.ValidString(password) {
		return password
	}

	// Map
	mapped := make([]rune, 0, len(password))
	for _, char := range password {
		switch {
		case stringprep.TableC1_2.Contains(char):
			mapped = append(mapped, ' ')
		case saslprepMappedToNothing.Contains(char):
		default:
			mapped = append(mapped, char)
		}
	}
	if len(mapped) == 0 {
		return password
	}

	// Prohibit, and check bidi (RFC 3454, section 6): a string with a
	// right-to-left character must not have a left-to-right one, and must
	// start and end with a right-to-left character.
	rightToLeft, leftToRight := false, false
	for _, char := range mapped {
		for _, prohibited := range saslprepProhibited {
			if prohibited.Contains(char) {
				return password
			}
		}
		rightToLeft = rightToLeft || stringprep.TableD1.Contains(char)
		leftToRight = leftToRight || stringprep.TableD2.Contains(char)
	}
	if rightToLeft && (leftToRight ||
		!stringprep.TableD1.Contains(mapped[0]) ||
		!stringprep.TableD1.Contains(mapped[len(mapped)-1])) {
		return password
	}

	// Normalize
	return nfkc(string(mapped))
}

// nfkc returns the Unicode normalization form KC of a string, like
// unicode_normalize() in src/common/unicode_norm.c.
//
// norm.NFKC cannot be used for it, its composition is wrong for some sequences.
// It looks compositions up by the low 16 bits of the two characters: U+20041
// U+0301 gives U+00C1, the composition of U+0041 and U+0301, and U+05D2 U+0307
// gives U+105C9, the composition of U+105D2 and U+0307. It also composes a
// character with a combining character another starter separates it from: "I"
// U+09BE U+0316 U+0307 gives U+0130 U+09BE U+0316. So only its decomposition
// is used, and the result is recomposed the way PostgreSQL does it.
func nfkc(s string) string {
	decomposed := []rune(norm.NFKD.String(s))
	if len(decomposed) == 0 {
		return ""
	}

	composed := make([]rune, 1, len(decomposed))
	composed[0] = decomposed[0]
	starter, lastClass := 0, -1
	for _, char := range decomposed[1:] {
		class := int(norm.NFKD.PropertiesString(string(char)).CCC())
		if lastClass < class {
			if composite, ok := composePair(composed[starter], char); ok {
				composed[starter] = composite
				continue
			}
		}
		if class == 0 {
			starter, lastClass = len(composed), -1
		} else {
			lastClass = class
		}
		composed = append(composed, char)
	}
	return string(composed)
}

// composePair returns the character two characters compose into, if any.
func composePair(first, second rune) (rune, bool) {
	pair := string([]rune{first, second})
	composed := norm.NFC.String(pair)
	// A composition is equivalent to what it composes, which rules out the
	// wrong ones of norm.NFC.
	if utf8.RuneCountInString(composed) != 1 || norm.NFD.String(composed) != norm.NFD.String(pair) {
		return 0, false
	}

	composite, _ := utf8.DecodeRuneInString(composed)
	return composite, true
}

func isASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= utf8.RuneSelf {
			return false
		}
	}
	return true
}
