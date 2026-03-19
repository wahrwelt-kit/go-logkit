package logkit

import (
	"bytes"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"
)

func captureLogger(t *testing.T) (Logger, *bytes.Buffer) {
	t.Helper()
	l, err := New(WithLevel(InfoLevel), WithOutput(ConsoleOutput))
	require.NoError(t, err)
	buf := &bytes.Buffer{}
	zl := l.(*zerologLogger)
	zl.zl = zl.zl.Output(buf)
	return zl, buf
}

func TestSlogHandler(t *testing.T) {
	t.Parallel()
	l, buf := captureLogger(t)
	slogLogger := slog.New(SlogHandler(l))
	slogLogger.Info("hello from slog", "key", "value")
	require.Contains(t, buf.String(), "hello from slog")
	require.Contains(t, buf.String(), "value")
}

func TestSlogHandler_WithGroup_PrefixesRecordAttrs(t *testing.T) {
	t.Parallel()
	l, buf := captureLogger(t)
	h := SlogHandler(l).WithGroup("g")
	slogLogger := slog.New(h)
	slogLogger.Info("msg", "a", "1")
	out := buf.String()
	require.Contains(t, out, "msg")
	require.Contains(t, out, "g.a")
	require.Contains(t, out, "1")
}

func TestSlogHandler_WithGroup_WithAttrs_ThenHandle(t *testing.T) {
	t.Parallel()
	l, buf := captureLogger(t)
	h := SlogHandler(l).WithGroup("req").WithAttrs([]slog.Attr{slog.String("id", "r1")})
	slogLogger := slog.New(h)
	slogLogger.Info("handled", "status", 200)
	out := buf.String()
	require.Contains(t, out, "handled")
	require.Contains(t, out, "req.id")
	require.Contains(t, out, "r1")
	require.Contains(t, out, "req.status")
	require.Contains(t, out, "200")
}

func TestSlogHandler_WithGroup_NestedGroups(t *testing.T) {
	t.Parallel()
	l, buf := captureLogger(t)
	h := SlogHandler(l).WithGroup("a").WithGroup("b")
	slogLogger := slog.New(h)
	slogLogger.Info("nested", "k", "v")
	out := buf.String()
	require.Contains(t, out, "nested")
	require.Contains(t, out, "a.b.k")
	require.Contains(t, out, "v")
}
