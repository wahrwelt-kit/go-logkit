package logger

import (
	"errors"
	"testing"

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
	require.True(t, errors.Is(err, ErrEmptyFilename))
	require.Nil(t, l)
}

func TestNew_BothOutput_EmptyFilename_Error(t *testing.T) {
	t.Parallel()
	l, err := New(WithOutput(BothOutput))
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrEmptyFilename))
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
	assert.Equal(t, ErrorLevel, opts.Level)
	assert.Equal(t, FileOutput, opts.Output)
	assert.Equal(t, "/tmp/log", opts.FileOptions.Filename)
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

func TestNew_FileOutput_Success(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	filename := dir + "/log.txt"
	l, err := New(WithOutput(FileOutput), WithFileOptions(FileOptions{Filename: filename}))
	require.NoError(t, err)
	require.NotNil(t, l)
	l.Info("file test")
}

func TestZerologLogger_Log_MergesMultipleFields(t *testing.T) {
	t.Parallel()
	l, err := New(WithLevel(InfoLevel), WithOutput(ConsoleOutput))
	require.NoError(t, err)
	l.Info("merged", Fields{"a": 1}, Fields{"b": 2})
	l.Info("override", Fields{"k": "first"}, Fields{"k": "second"})
}
