package postgresql

import (
	"net/url"
	"sort"
	"strings"
)

const redactedPlaceholder = "XXXX"

// redactPassword returns s with every known textual form of password replaced
// by redactedPlaceholder. An empty password is a no-op: replacing the empty
// string would insert redactedPlaceholder between every byte of s.
func redactPassword(s, password string) string {
	if password == "" {
		return s
	}
	for _, f := range passwordForms(password) {
		s = strings.ReplaceAll(s, f, redactedPlaceholder)
	}
	return s
}

// passwordForms returns the distinct textual encodings of password that may
// surface in a connection error, ordered longest-first.
//
// It is the closure of password under url.PathEscape and url.QueryEscape
// applied up to twice. This covers:
//
//   - the raw password;
//   - url.PathEscape(password) — the form connStr() embeds in the DSN;
//   - url.QueryEscape(password) — the form some wrappers re-encode into;
//   - each of those escaped again — a wrapper percent-encoding an already
//     escaped DSN turns '%' into %25, e.g. p%40ss -> p%2540ss.
//
// Longest-first ordering ensures a longer encoding is redacted before any
// shorter form that might be a substring of it.
func passwordForms(password string) []string {
	escapers := []func(string) string{url.PathEscape, url.QueryEscape}

	set := map[string]struct{}{password: {}}

	// First pass: single escapes of the raw password.
	singles := make([]string, 0, len(escapers))
	for _, esc := range escapers {
		f := esc(password)
		if _, ok := set[f]; !ok {
			set[f] = struct{}{}
			singles = append(singles, f)
		}
	}
	// Second pass: escape each single form again (covers all double encodings).
	for _, base := range singles {
		for _, esc := range escapers {
			set[esc(base)] = struct{}{}
		}
	}

	forms := make([]string, 0, len(set))
	for f := range set {
		if f != "" {
			forms = append(forms, f)
		}
	}
	sort.Slice(forms, func(i, j int) bool {
		if len(forms[i]) != len(forms[j]) {
			return len(forms[i]) > len(forms[j])
		}
		return forms[i] < forms[j]
	})
	return forms
}

// redactedError wraps an error so its message is redacted while the underlying
// cause remains accessible through errors.Is / errors.As. This preserves the
// error chain that callers may rely on.
type redactedError struct {
	inner    error
	password string
}

func (e *redactedError) Error() string {
	return redactPassword(e.inner.Error(), e.password)
}

func (e *redactedError) Unwrap() error { return e.inner }

// wrapRedacted returns err wrapped in a redactedError. nil in, nil out.
func wrapRedacted(err error, password string) error {
	if err == nil {
		return nil
	}
	return &redactedError{inner: err, password: password}
}
