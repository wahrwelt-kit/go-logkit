package logkit

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
)

func FuzzSanitizeMsg(f *testing.F) {
	for _, seed := range []string{
		"clean",
		"line\nbreak",
		"carriage\rreturn",
		"null\x00byte",
		"unicode\u2028separator",
		"unicode\u2029separator",
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, input string) {
		out := sanitizeMsg(input)
		if hasUnsafeLogRune(out) {
			t.Fatalf("sanitizeMsg left unsafe rune: %q", out)
		}
	})
}

func FuzzSanitizeFields(f *testing.F) {
	seeds := [][2]string{
		{"safe", testFieldValue},
		{keyAuthorization, "Bearer " + testSecretValue},
		{keySetCookie, "session=" + testSecretValue},
		{keyPrivateKey, testSecretValue},
		{"message\nkey", "line\nbreak"},
	}
	for _, seed := range seeds {
		f.Add(seed[0], seed[1])
	}
	f.Fuzz(func(t *testing.T, key, value string) {
		out := sanitizeFields(Fields{key: value})
		for k, v := range out {
			if hasUnsafeLogRune(k) {
				t.Fatalf("sanitizeFields left unsafe key rune: %q", k)
			}
			if s, ok := v.(string); ok && hasUnsafeLogRune(s) {
				t.Fatalf("sanitizeFields left unsafe value rune: %q", s)
			}
		}
	})
}

func FuzzSensitiveKeyMatching(f *testing.F) {
	for _, seed := range []string{
		keyAuthorization,
		keyProxyAuthorization,
		"set-cookie",
		"session.id",
		"csrf_token",
		"xsrf-token",
		"private key",
		"normal",
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, key string) {
		out := sanitizeFields(Fields{key: testSecretValue})
		for sanitizedKey, value := range out {
			if isSensitiveKey(sanitizedKey) && value != redactedValue {
				t.Fatalf("sensitive key %q was not redacted: %#v", sanitizedKey, value)
			}
		}
	})
}

func FuzzSlogAttrs(f *testing.F) {
	seeds := [][2]string{
		{"key", testFieldValue},
		{"http.method", "GET"},
		{keyAuthorization, "Bearer " + testSecretValue},
		{"line\nkey", "line\nvalue"},
	}
	for _, seed := range seeds {
		f.Add(seed[0], seed[1])
	}
	f.Fuzz(func(t *testing.T, key, value string) {
		buf := &bytes.Buffer{}
		l, err := New(WithWriter(buf))
		if err != nil {
			t.Fatal(err)
		}
		slog.New(SlogHandler(l)).InfoContext(context.Background(), "fuzz", key, value)
		out := strings.TrimSuffix(buf.String(), "\n")
		if hasUnsafeLogRune(out) {
			t.Fatalf("slog output has unsafe rune: %q", out)
		}
	})
}

func hasUnsafeLogRune(s string) bool {
	return strings.ContainsAny(s, "\x00\r\n\u2028\u2029")
}
