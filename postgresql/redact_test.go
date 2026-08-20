package postgresql

import (
	"errors"
	"net/url"
	"strings"
	"testing"
)

func TestRedactPassword(t *testing.T) {
	cases := []struct {
		name     string
		input    string
		password string
		wantOmit []string
		wantHave []string
	}{
		{
			name:     "empty password is a no-op",
			input:    "foo bar baz",
			password: "",
			wantOmit: nil,
			wantHave: []string{"foo bar baz"},
		},
		{
			name:     "raw form is replaced",
			input:    "connection string: postgres://u:simple@h/db",
			password: "simple",
			wantOmit: []string{"simple"},
			wantHave: []string{redactedPlaceholder},
		},
		{
			name:     "url.PathEscape form is replaced",
			input:    `parse "postgres://postgres:p@ss:w%2Ford%231@bad host with space:55432/postgres": invalid character " " in host name`,
			password: "p@ss:w/ord#1",
			wantOmit: []string{"p@ss:w/ord#1", "p@ss:w%2Ford%231"},
			wantHave: []string{redactedPlaceholder},
		},
		{
			name:     "url.QueryEscape form (space encoded as +) is replaced",
			input:    "trace: u=foo+bar in field",
			password: "foo bar",
			wantOmit: []string{"foo bar", "foo+bar"},
			wantHave: []string{redactedPlaceholder},
		},
		{
			name:     "double-escaped form is replaced",
			input:    `wrapped: postgres://u:p%2540ss@h/db`,
			password: "p@ss",
			wantOmit: []string{"p@ss", "p%40ss", "p%2540ss"},
			wantHave: []string{redactedPlaceholder},
		},
		{
			name:     "space-only password (PathEscape: %20)",
			input:    "trace: contains%20space marker",
			password: "contains space",
			wantOmit: []string{"contains space", "contains%20space"},
			wantHave: []string{redactedPlaceholder},
		},
		{
			name:     "multiple occurrences (> 2) all replaced",
			input:    "a=plain b=plain c=plain d=plain",
			password: "plain",
			wantOmit: []string{"plain"},
			wantHave: []string{redactedPlaceholder},
		},
		{
			name:     "unicode password (PathEscape form differs from raw)",
			input:    "user=unicode_%C3%A9_%C3%B1 fail",
			password: "unicode_é_ñ",
			wantOmit: []string{"unicode_é_ñ", "unicode_%C3%A9_%C3%B1"},
			wantHave: []string{redactedPlaceholder},
		},
		{
			name:     "password is also a substring of an unrelated word",
			input:    "superplain pattern",
			password: "plain",
			wantOmit: []string{"plain"},
			wantHave: []string{"super", " pattern"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := redactPassword(tc.input, tc.password)
			for _, s := range tc.wantOmit {
				if strings.Contains(got, s) {
					t.Errorf("output contains forbidden substring %q\ngot:   %q\nin:    %q", s, got, tc.input)
				}
			}
			for _, s := range tc.wantHave {
				if !strings.Contains(got, s) {
					t.Errorf("output missing required substring %q\ngot: %q", s, got)
				}
			}
		})
	}
}

func TestRedactedError_PreservesUnwrap(t *testing.T) {
	type sentinel struct{ msg string }
	// Use stdlib errors.Is via a known sentinel error.
	base := errorWithMessage("pq: connection refused")
	wrapped := wrapRedacted(base, "irrelevant")

	if wrapped == nil {
		t.Fatal("wrapRedacted returned nil for a non-nil error")
	}
	if !errors.Is(wrapped, base) {
		t.Errorf("errors.Is should walk the chain; expected to find base error")
	}
	_ = sentinel{}
}

func TestRedactedError_RedactsOnError(t *testing.T) {
	const password = "p@ss:w/ord#1"
	encoded := url.PathEscape(password)
	base := errorWithMessage("parse \"postgres://u:" + encoded + "@h/db\": bad")
	wrapped := wrapRedacted(base, password)

	got := wrapped.Error()
	for _, s := range []string{password, encoded} {
		if strings.Contains(got, s) {
			t.Errorf("redactedError.Error() leaks %q\ngot: %q", s, got)
		}
	}
	if !strings.Contains(got, redactedPlaceholder) {
		t.Errorf("redactedError.Error() missing placeholder\ngot: %q", got)
	}
}

func TestWrapRedacted_NilPassthrough(t *testing.T) {
	if got := wrapRedacted(nil, "anything"); got != nil {
		t.Errorf("wrapRedacted(nil, …) should return nil, got %v", got)
	}
}

func FuzzRedactPassword(f *testing.F) {
	f.Add("error: postgres://u:p%40ss@h/db", "p@ss")
	f.Add("trace: 3 occurrences of plain plain plain", "plain")
	f.Add("unicode: u=%C3%A9", "é")
	f.Add("", "anything")
	f.Add("no secret here", "")

	f.Fuzz(func(t *testing.T, surrounding, password string) {
		got := redactPassword(surrounding, password)
		if password == "" {
			if got != surrounding {
				t.Fatalf("empty password should be no-op, got %q", got)
			}
			return
		}
		forms := []string{
			password,
			url.PathEscape(password),
			url.QueryEscape(password),
			url.PathEscape(url.PathEscape(password)),
		}
		for _, form := range forms {
			if form == "" {
				continue
			}
			if strings.Contains(got, form) {
				t.Errorf("output leaks form %q of password %q\ngot: %q\nfrom: %q", form, password, got, surrounding)
			}
		}
	})
}

// TestConnect_RedactsPasswordInError drives the real Connect() path with a
// URL-special password and a malformed host. The unescaped host makes the
// driver's url.Parse fail and echo the full DSN — including the PathEscaped
// password — which is exactly the leak the legacy strings.Replace missed.
// It asserts none of the password's textual forms survive in the returned
// error and that the placeholder is present.
func TestConnect_RedactsPasswordInError(t *testing.T) {
	const password = "p@ss/word:1" // URL-special: @ / :
	cfg := &Config{
		Scheme:            "postgres",
		Host:              "bad host with space", // unescaped -> url.Parse fails -> DSN echoed
		Port:              5432,
		Username:          "app",
		Password:          password,
		SSLMode:           "disable",
		ConnectTimeoutSec: 1,
		ApplicationName:   "tf-redact-test",
	}

	_, err := cfg.NewClient("postgres").Connect()
	if err == nil {
		t.Fatal("expected a connection error from a malformed host, got nil")
	}

	msg := err.Error()
	for _, form := range passwordForms(password) {
		if strings.Contains(msg, form) {
			t.Errorf("connection error leaks password form %q\nerr: %s", form, msg)
		}
	}
	if !strings.Contains(msg, redactedPlaceholder) {
		t.Errorf("expected redacted placeholder %q in error, got: %s", redactedPlaceholder, msg)
	}
}

// TestRedactPassword_AllSpecialChars covers success-criterion B: every printable
// ASCII character that url.PathEscape or url.QueryEscape rewrites must be
// redacted when it appears in a password embedded in a connection error — in
// both the PathEscape form (what connStr embeds) and the QueryEscape form (what
// a re-encoding wrapper may produce). Iterating the whole range guards against
// the form list silently missing a single character.
func TestRedactPassword_AllSpecialChars(t *testing.T) {
	var dangerous []rune
	for r := rune(0x20); r < 0x7f; r++ {
		s := string(r)
		if url.PathEscape(s) != s || url.QueryEscape(s) != s {
			dangerous = append(dangerous, r)
		}
	}
	if len(dangerous) < 20 {
		t.Fatalf("expected many dangerous chars, found %d", len(dangerous))
	}

	check := func(t *testing.T, password string) {
		t.Helper()
		// Both encodings that can surface in an error for the same password.
		for _, embedded := range []string{url.PathEscape(password), url.QueryEscape(password)} {
			errMsg := `parse "postgres://app:` + embedded + `@h:5432/db": driver error`
			got := redactPassword(errMsg, password)
			for _, form := range passwordForms(password) {
				if strings.Contains(got, form) {
					t.Errorf("form %q survived redaction\npassword: %q\nembedded: %q\ngot: %q", form, password, embedded, got)
				}
			}
			if !strings.Contains(got, redactedPlaceholder) {
				t.Errorf("placeholder missing\npassword: %q\nembedded: %q\ngot: %q", password, embedded, got)
			}
		}
	}

	// One char at a time, anchored between safe bytes to mimic a real secret.
	for _, r := range dangerous {
		c := string(r)
		t.Run("char_"+url.QueryEscape(c), func(t *testing.T) {
			check(t, "a"+c+"b")
		})
	}

	// Every dangerous char at once.
	t.Run("all_at_once", func(t *testing.T) {
		var sb strings.Builder
		for _, r := range dangerous {
			sb.WriteRune(r)
		}
		check(t, "pre"+sb.String()+"post")
	})

	// Unicode (multi-byte percent-encoding, e.g. é -> %C3%A9).
	t.Run("unicode", func(t *testing.T) {
		check(t, "ünîçødé_é_ñ")
	})
}

// errorWithMessage is a tiny test helper to build an error with a deterministic
// message without depending on fmt.Errorf's wrapping rules.
type stringError string

func (e stringError) Error() string { return string(e) }

func errorWithMessage(msg string) error { return stringError(msg) }
