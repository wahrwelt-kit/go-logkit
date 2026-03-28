package logkit

import (
	"bytes"
	"context"
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

func TestSlogHandler_LevelMapping(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		logFn   func(*slog.Logger)
		wantLvl string
	}{
		{"warn", func(sl *slog.Logger) { sl.Warn("msg") }, `"level":"warn"`},
		{"error", func(sl *slog.Logger) { sl.Error("msg") }, `"level":"error"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			l, buf := captureLogger(t)
			tt.logFn(slog.New(SlogHandler(l)))
			require.Contains(t, buf.String(), tt.wantLvl)
		})
	}
}

func TestSlogHandler_LevelMapping_Debug(t *testing.T) {
	t.Parallel()
	l, err := New(WithLevel(DebugLevel), WithOutput(ConsoleOutput))
	require.NoError(t, err)
	buf := &bytes.Buffer{}
	zl := l.(*zerologLogger)
	zl.zl = zl.zl.Output(buf)
	slog.New(SlogHandler(zl)).Debug("msg")
	require.Contains(t, buf.String(), `"level":"debug"`)
}

func TestSlogHandler_GroupAttrInRecord(t *testing.T) {
	t.Parallel()
	l, buf := captureLogger(t)
	slog.New(SlogHandler(l)).Info("request",
		slog.Group("http", slog.String("method", "GET"), slog.String("path", "/api")),
	)
	out := buf.String()
	require.Contains(t, out, "http.method")
	require.Contains(t, out, "GET")
	require.Contains(t, out, "http.path")
	require.Contains(t, out, "/api")
}

func TestSlogHandler_WithAttrs_GroupAttr(t *testing.T) {
	t.Parallel()
	l, buf := captureLogger(t)
	h := SlogHandler(l).WithAttrs([]slog.Attr{
		slog.Group("db", slog.String("host", "localhost"), slog.Int("port", 5432)),
	})
	slog.New(h).Info("connected")
	out := buf.String()
	require.Contains(t, out, "db.host")
	require.Contains(t, out, "localhost")
	require.Contains(t, out, "db.port")
}

func TestSlogHandler_Enabled_WithLeveler(t *testing.T) {
	t.Parallel()
	l, err := New(WithLevel(WarnLevel))
	require.NoError(t, err)
	h := SlogHandler(l)
	ctx := context.Background()
	require.False(t, h.Enabled(ctx, slog.LevelDebug))
	require.False(t, h.Enabled(ctx, slog.LevelInfo))
	require.True(t, h.Enabled(ctx, slog.LevelWarn))
	require.True(t, h.Enabled(ctx, slog.LevelError))
}

func TestSlogHandler_Enabled_NoLeveler(t *testing.T) {
	t.Parallel()
	h := SlogHandler(Noop())
	ctx := context.Background()
	require.True(t, h.Enabled(ctx, slog.LevelDebug))
	require.True(t, h.Enabled(ctx, slog.LevelError))
}
