package logkit

import (
	"errors"
	"io"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNew_Success(t *testing.T) {
	t.Parallel()
	l, err := New(WithLevel(InfoLevel), WithOutput(ConsoleOutput))
	require.NoError(t, err)
	require.NotNil(t, l)
	l.Info("test")
}

func TestNew_NoOptions(t *testing.T) {
	t.Parallel()
	l, err := New()
	require.NoError(t, err)
	require.NotNil(t, l)
	l.Info("test")
}

func TestNew_FileOutput_EmptyFilename_Error(t *testing.T) {
	t.Parallel()
	l, err := New(WithOutput(FileOutput))
	require.Error(t, err)
	require.ErrorIs(t, err, ErrEmptyFilename)
	require.Nil(t, l)
}

func TestNew_BothOutput_EmptyFilename_Error(t *testing.T) {
	t.Parallel()
	l, err := New(WithOutput(BothOutput))
	require.Error(t, err)
	require.ErrorIs(t, err, ErrEmptyFilename)
	require.Nil(t, l)
}

func TestNew_DefaultOutput(t *testing.T) {
	t.Parallel()
	l, err := New(WithLevel(DebugLevel), WithOutput(OutputType(99)))
	require.NoError(t, err)
	require.NotNil(t, l)
	l.Debug("test")
}

func TestZerologLogger_WithError_Success(t *testing.T) {
	t.Parallel()
	l, err := New(WithLevel(InfoLevel), WithOutput(ConsoleOutput))
	require.NoError(t, err)
	l = l.WithError(errors.New("err"))
	require.NotNil(t, l)
	l.Info("with error")
}

func TestZerologLogger_WithFields_Success(t *testing.T) {
	t.Parallel()
	l, err := New(WithLevel(InfoLevel), WithOutput(ConsoleOutput))
	require.NoError(t, err)
	l = l.WithFields(Fields{"k": "v"})
	require.NotNil(t, l)
	l.Info("with fields")
}

func TestConvertLogLevel_Default(t *testing.T) {
	t.Parallel()
	l, err := New(WithLevel(Level(100)), WithOutput(ConsoleOutput))
	require.NoError(t, err)
	require.NotNil(t, l)
	l.Info("default level")
}

func TestOptions_Apply(t *testing.T) {
	t.Parallel()
	opts := &Options{}
	WithLevel(ErrorLevel)(opts)
	WithOutput(FileOutput)(opts)
	WithFileOptions(FileOptions{Filename: "/tmp/log"})(opts)
	WithServiceName("api")(opts)
	assert.Equal(t, ErrorLevel, opts.Level)
	assert.Equal(t, FileOutput, opts.Output)
	assert.Equal(t, "/tmp/log", opts.FileOptions.Filename)
	assert.Equal(t, "api", opts.ServiceName)
}

func TestNoop_AllMethods(t *testing.T) {
	t.Parallel()
	l := Noop()
	require.NotNil(t, l)
	l.Debug("debug")
	l.Info("info")
	l.Warn("warn")
	l.Error("error")
	l.Fatal("fatal")
	l.WithFields(Fields{"a": 1}).Info("with fields")
	l.WithError(errors.New("err")).Info("with error")
}

func TestZerologLogger_Fatal_WithExitFunc_DoesNotExit(t *testing.T) {
	t.Parallel()
	called := false
	l, err := New(WithLevel(InfoLevel), WithOutput(ConsoleOutput), WithExitFunc(func(_ int) {
		called = true
	}))
	require.NoError(t, err)
	l.Fatal("fatal")
	require.True(t, called)
}

func TestNew_FileOutput_Success(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	filename := dir + "/log.txt"
	l, err := New(WithOutput(FileOutput), WithFileOptions(FileOptions{Filename: filename}))
	require.NoError(t, err)
	require.NotNil(t, l)
	t.Cleanup(func() { _ = l.Close() })
	l.Info("file test")
}

func TestZerologLogger_Log_MergesMultipleFields(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	filename := dir + "/merge.log"
	l, err := New(WithLevel(InfoLevel), WithOutput(FileOutput), WithFileOptions(FileOptions{Filename: filename}))
	require.NoError(t, err)
	t.Cleanup(func() { _ = l.Close() })
	l.Info("merged", Fields{"a": 1}, Fields{"b": 2})
	l.Info("override", Fields{"k": "first"}, Fields{"k": "second"})
	_ = l.Close()
	data, err := os.ReadFile(filename)
	require.NoError(t, err)
	content := string(data)
	require.Contains(t, content, "merged")
	require.Contains(t, content, "a")
	require.Contains(t, content, "b")
	require.Contains(t, content, "override")
	require.Contains(t, content, "second")
}

func TestSanitizeMsg(t *testing.T) {
	t.Parallel()
	const helloWorld = "hello world"
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"newline", "hello\nworld", helloWorld},
		{"carriage return", "hello\rworld", helloWorld},
		{"null byte", "hello\x00world", helloWorld},
		{"unicode line sep", "hello\u2028world", helloWorld},
		{"unicode para sep", "hello\u2029world", helloWorld},
		{"tabs", "hello\tworld", helloWorld},
		{"clean", helloWorld, helloWorld},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := sanitizeMsg(tt.in)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestSanitizeFields(t *testing.T) {
	t.Parallel()
	err := errors.New("test\nerror")
	tests := []struct {
		name string
		in   Fields
	}{
		{"string", Fields{"s": "a\nb"}},
		{"int", Fields{"n": 42}},
		{keyError, Fields{"err": err}},
		{"stringer", Fields{"d": time.Second}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			out := sanitizeFields(tt.in)
			require.Len(t, out, len(tt.in))
			for k, v := range out {
				assert.NotContains(t, k, "\n")
				_, ok := tt.in[k]
				assert.True(t, ok)
				if s, ok := v.(string); ok {
					assert.NotContains(t, s, "\n")
				}
			}
		})
	}
}

func TestSanitizeFields_RedactsSensitiveKeys(t *testing.T) {
	t.Parallel()
	out := sanitizeFields(Fields{
		keyAuthorization:      "Bearer " + testSecretValue,
		keyProxyAuthorization: "Basic " + testSecretValue,
		keyCookie:             "session=" + testSecretValue,
		keySetCookie:          "session=" + testSecretValue,
		keyPassword:           testSecretValue,
		keyAPIKey:             testSecretValue,
		keyAccessToken:        testSecretValue,
		"session_id":          testSecretValue,
		"csrf":                testSecretValue,
		"xsrf":                testSecretValue,
		keyPrivateKey:         testSecretValue,
		"safe":                "visible",
	})
	require.Equal(t, redactedValue, out[keyAuthorization])
	require.Equal(t, redactedValue, out[keyProxyAuthorization])
	require.Equal(t, redactedValue, out[keyCookie])
	require.Equal(t, redactedValue, out[keySetCookie])
	require.Equal(t, redactedValue, out[keyPassword])
	require.Equal(t, redactedValue, out[keyAPIKey])
	require.Equal(t, redactedValue, out[keyAccessToken])
	require.Equal(t, redactedValue, out["session_id"])
	require.Equal(t, redactedValue, out["csrf"])
	require.Equal(t, redactedValue, out["xsrf"])
	require.Equal(t, redactedValue, out[keyPrivateKey])
	require.Equal(t, "visible", out["safe"])
}

func TestSanitizeFields_DoesNotRedactTokenMetrics(t *testing.T) {
	t.Parallel()
	out := sanitizeFields(Fields{
		"github_token": testSecretValue,
		"token_count":  42,
		"token_type":   "access",
	})
	require.Equal(t, redactedValue, out["github_token"])
	require.Equal(t, 42, out["token_count"])
	require.Equal(t, "access", out["token_type"])
}

func TestWithSensitiveKeys_RedactsAdditionalKeys(t *testing.T) {
	t.Parallel()
	buf := &syncBuffer{}
	l, err := New(
		WithWriter(buf),
		WithSensitiveKeys("tenant_license"),
	)
	require.NoError(t, err)
	l.Info("configured redaction", Fields{"tenant_license": testSecretValue, "tenant": "acme"})
	out := buf.String()
	require.Contains(t, out, `"tenant_license":"[REDACTED]"`)
	require.NotContains(t, out, testSecretValue)
	require.Contains(t, out, `"tenant":"acme"`)
}

func TestWithRedactor_CustomReplacement(t *testing.T) {
	t.Parallel()
	buf := &syncBuffer{}
	l, err := New(
		WithWriter(buf),
		WithRedactor(func(key string, value any) (any, bool) {
			if key == "email" {
				return "redacted@example.invalid", true
			}
			return value, false
		}),
	)
	require.NoError(t, err)
	l.WithFields(Fields{"email": "user@example.com"}).Info("custom redactor")
	out := buf.String()
	require.Contains(t, out, `"email":"redacted@example.invalid"`)
	require.NotContains(t, out, "user@example.com")
}

type jsonSecretPayload struct{}

func (jsonSecretPayload) MarshalJSON() ([]byte, error) {
	return []byte(`{"safe":"ok","token":"secret-token","nested":{"private_key":"secret-key"},"items":[{"csrf":"secret-csrf"}]}`), nil
}

type jsonRedactorPayload struct{}

func (jsonRedactorPayload) MarshalJSON() ([]byte, error) {
	return []byte(`{"custom_nested":"replace-me"}`), nil
}

type jsonCustomSensitivePayload struct{}

func (jsonCustomSensitivePayload) MarshalJSON() ([]byte, error) {
	return []byte(`{"custom_secret":"custom-secret","safe":"ok"}`), nil
}

func TestJSONMarshaler_RedactsNestedSensitiveKeys(t *testing.T) {
	t.Parallel()
	buf := &syncBuffer{}
	l, err := New(WithWriter(buf))
	require.NoError(t, err)
	l.Info("json payload", Fields{testPayloadKey: jsonSecretPayload{}})
	out := buf.String()
	require.Contains(t, out, `"safe":"ok"`)
	require.Contains(t, out, `"token":"[REDACTED]"`)
	require.Contains(t, out, `"private_key":"[REDACTED]"`)
	require.Contains(t, out, `"csrf":"[REDACTED]"`)
	require.NotContains(t, out, "secret-token")
	require.NotContains(t, out, "secret-key")
	require.NotContains(t, out, "secret-csrf")
}

func TestJSONMarshaler_CustomRedactorUsesConfiguredSensitiveKeys(t *testing.T) {
	t.Parallel()
	buf := &syncBuffer{}
	l, err := New(
		WithWriter(buf),
		WithSensitiveKeys("custom_secret"),
		WithRedactor(func(key string, value any) (any, bool) {
			if key == "custom_nested" {
				return jsonCustomSensitivePayload{}, true
			}
			return value, false
		}),
	)
	require.NoError(t, err)
	l.Info("json payload", Fields{testPayloadKey: jsonRedactorPayload{}})
	out := buf.String()
	require.Contains(t, out, `"custom_secret":"[REDACTED]"`)
	require.Contains(t, out, `"safe":"ok"`)
	require.NotContains(t, out, "custom-secret")
}

func TestJSONMarshaler_TopLevelSensitiveKeyRedactsWholeValue(t *testing.T) {
	t.Parallel()
	buf := &syncBuffer{}
	l, err := New(WithWriter(buf))
	require.NoError(t, err)
	l.Info("json payload", Fields{"secret_payload": jsonSecretPayload{}})
	out := buf.String()
	require.Contains(t, out, `"secret_payload":"[REDACTED]"`)
	require.NotContains(t, out, `"safe":"ok"`)
}

type invalidJSONMarshaler struct{}

func (invalidJSONMarshaler) MarshalJSON() ([]byte, error) {
	return []byte(`{"token":`), nil
}

func TestJSONMarshaler_InvalidJSONFallsBackToString(t *testing.T) {
	t.Parallel()
	l, err := New(WithWriter(io.Discard))
	require.NoError(t, err)
	require.NotPanics(t, func() {
		l.Info("invalid json", Fields{"payload": invalidJSONMarshaler{}})
	})
}

func TestWithError_Nil_NoErrorField(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	filename := dir + "/noerr.log"
	l, err := New(WithLevel(InfoLevel), WithOutput(FileOutput), WithFileOptions(FileOptions{Filename: filename}))
	require.NoError(t, err)
	t.Cleanup(func() { _ = l.Close() })
	child := l.WithError(nil)
	child.Info("msg", Fields{"k": "v"})
	data, err := os.ReadFile(filename)
	require.NoError(t, err)
	require.Contains(t, string(data), "msg")
	require.Contains(t, string(data), "k")
	require.NotContains(t, string(data), `"error":`)
}

func TestClosedLoggerSilentlyDrops(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	filename := dir + "/closed.log"
	l, err := New(WithOutput(FileOutput), WithFileOptions(FileOptions{Filename: filename}))
	require.NoError(t, err)
	l.Info("first write")
	require.NoError(t, l.Close())
	l.Info("should not appear")
	data, err := os.ReadFile(filename)
	require.NoError(t, err)
	require.Contains(t, string(data), "first write")
	require.NotContains(t, string(data), "should not appear")
}

func TestClose_NoCloserStopsFurtherWrites(t *testing.T) {
	t.Parallel()
	buf := &syncBuffer{}
	l, err := New(WithWriter(buf))
	require.NoError(t, err)
	l.Info("before close")
	require.NoError(t, l.Close())
	l.Info("after close")
	out := buf.String()
	require.Contains(t, out, "before close")
	require.NotContains(t, out, "after close")
}

func TestBothOutput_WritesToFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	filename := dir + "/both.log"
	l, err := New(WithLevel(InfoLevel), WithOutput(BothOutput), WithFileOptions(FileOptions{Filename: filename}))
	require.NoError(t, err)
	t.Cleanup(func() { _ = l.Close() })
	l.Info("both test")
	data, err := os.ReadFile(filename)
	require.NoError(t, err)
	require.Contains(t, string(data), "both test")
}

func TestServiceName_AddsServiceField(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	filename := dir + "/svc.log"
	l, err := New(WithLevel(InfoLevel), WithOutput(FileOutput), WithFileOptions(FileOptions{Filename: filename}), WithServiceName("myservice"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = l.Close() })
	l.Info("started")
	data, err := os.ReadFile(filename)
	require.NoError(t, err)
	require.Contains(t, string(data), "myservice")
	require.Contains(t, string(data), "started")
}

func TestCaller_InOutput(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	filename := dir + "/caller.log"
	l, err := New(WithLevel(InfoLevel), WithOutput(FileOutput), WithFileOptions(FileOptions{Filename: filename}))
	require.NoError(t, err)
	t.Cleanup(func() { _ = l.Close() })
	l.Info("caller test")
	data, err := os.ReadFile(filename)
	require.NoError(t, err)
	out := string(data)
	require.Contains(t, out, "caller test")
	require.Contains(t, out, "logger_test.go", "caller should point to the test file, not to logger.go inside logkit")
}

func TestFatal_CallerInOutput(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	filename := dir + "/fatal-caller.log"
	l, err := New(
		WithLevel(InfoLevel),
		WithOutput(FileOutput),
		WithFileOptions(FileOptions{Filename: filename}),
		WithExitFunc(func(_ int) {}),
	)
	require.NoError(t, err)
	l.Fatal("fatal caller test")
	data, err := os.ReadFile(filename)
	require.NoError(t, err)
	out := string(data)
	require.Contains(t, out, "fatal caller test")
	require.Contains(t, out, "logger_test.go", "Fatal caller should point to the test file, not logger.go")
}
