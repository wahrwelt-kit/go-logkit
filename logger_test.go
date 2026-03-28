package logkit

import (
	"errors"
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
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"newline", "hello\nworld", "hello world"},
		{"carriage return", "hello\rworld", "hello world"},
		{"null byte", "hello\x00world", "hello world"},
		{"unicode line sep", "hello\u2028world", "hello world"},
		{"unicode para sep", "hello\u2029world", "hello world"},
		{"tabs", "hello\tworld", "hello world"},
		{"clean", "hello world", "hello world"},
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
		{"error", Fields{"err": err}},
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
	require.Contains(t, string(data), "caller")
	require.Contains(t, string(data), "caller test")
}
