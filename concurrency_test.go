package logkit

import (
	"bytes"
	"context"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
)

type ctxKey string

const (
	keyTrace   ctxKey = "trace"
	keyRequest ctxKey = "request"
)

func TestWithContextExtractor_AddsFieldsFromContext(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	filename := dir + "/ctx-extract.log"
	l, err := New(
		WithLevel(InfoLevel),
		WithOutput(FileOutput),
		WithFileOptions(FileOptions{Filename: filename}),
		WithContextExtractor(func(ctx context.Context) Fields {
			f := Fields{}
			if v, ok := ctx.Value(keyTrace).(string); ok {
				f["trace_id"] = v
			}
			if v, ok := ctx.Value(keyRequest).(string); ok {
				f["request_id"] = v
			}
			return f
		}),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = l.Close() })
	ctx := context.WithValue(context.Background(), keyTrace, "trace-123")
	ctx = context.WithValue(ctx, keyRequest, "req-abc")
	l.InfoContext(ctx, "handled")
	require.NoError(t, l.Close())
	data, err := os.ReadFile(filename)
	require.NoError(t, err)
	out := string(data)
	require.Contains(t, out, `"trace_id":"trace-123"`)
	require.Contains(t, out, `"request_id":"req-abc"`)
	require.Contains(t, out, "handled")
}

func TestWithContextExtractor_NotInvokedByNonContextMethods(t *testing.T) {
	t.Parallel()
	called := atomic.Int64{}
	l, err := New(
		WithLevel(InfoLevel),
		WithOutput(ConsoleOutput),
		WithContextExtractor(func(_ context.Context) Fields {
			called.Add(1)
			return Fields{"injected": true}
		}),
	)
	require.NoError(t, err)
	l.Info("plain")
	l.Debug("plain")
	l.Warn("plain")
	l.Error("plain")
	require.Zero(t, called.Load(), "extractor must not run for non-context methods")
}

func TestWithContextExtractor_UserFieldsOverrideExtracted(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	filename := dir + "/override.log"
	l, err := New(
		WithLevel(InfoLevel),
		WithOutput(FileOutput),
		WithFileOptions(FileOptions{Filename: filename}),
		WithContextExtractor(func(_ context.Context) Fields {
			return Fields{"trace_id": "from-extractor", "service": "extracted"}
		}),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = l.Close() })
	l.InfoContext(context.Background(), "msg", Fields{"trace_id": "from-user"})
	require.NoError(t, l.Close())
	data, err := os.ReadFile(filename)
	require.NoError(t, err)
	out := string(data)
	require.Contains(t, out, `"trace_id":"from-user"`)
	require.NotContains(t, out, `"trace_id":"from-extractor"`)
	require.Contains(t, out, `"service":"extracted"`)
}

func TestWithContextExtractor_NilCtxSkipsExtractor(t *testing.T) {
	t.Parallel()
	called := atomic.Int64{}
	l, err := New(
		WithLevel(InfoLevel),
		WithOutput(ConsoleOutput),
		WithContextExtractor(func(_ context.Context) Fields {
			called.Add(1)
			return nil
		}),
	)
	require.NoError(t, err)
	l.InfoContext(context.TODO(), "ok") // non-nil ctx; extractor still runs
	require.Equal(t, int64(1), called.Load())
	l.InfoContext(nil, "ok") //nolint:staticcheck // intentional: verify nil-ctx guard
	require.Equal(t, int64(1), called.Load(), "nil ctx must not invoke extractor")
}

func TestWithContextExtractor_PropagatesToChildLoggers(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	filename := dir + "/child.log"
	l, err := New(
		WithLevel(InfoLevel),
		WithOutput(FileOutput),
		WithFileOptions(FileOptions{Filename: filename}),
		WithContextExtractor(func(_ context.Context) Fields {
			return Fields{"tenant": "acme"}
		}),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = l.Close() })
	child := l.WithFields(Fields{"component": "billing"})
	child.InfoContext(context.Background(), "billed")
	require.NoError(t, l.Close())
	data, err := os.ReadFile(filename)
	require.NoError(t, err)
	out := string(data)
	require.Contains(t, out, `"tenant":"acme"`)
	require.Contains(t, out, `"component":"billing"`)
	require.Contains(t, out, "billed")
}

func TestWithContextExtractor_MultipleExtractorsCompose(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	filename := dir + "/compose.log"
	l, err := New(
		WithLevel(InfoLevel),
		WithOutput(FileOutput),
		WithFileOptions(FileOptions{Filename: filename}),
		WithContextExtractor(func(_ context.Context) Fields { return Fields{"a": 1} }),
		WithContextExtractor(func(_ context.Context) Fields { return Fields{"b": 2} }),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = l.Close() })
	l.InfoContext(context.Background(), "msg")
	require.NoError(t, l.Close())
	data, err := os.ReadFile(filename)
	require.NoError(t, err)
	out := string(data)
	require.Contains(t, out, `"a":1`)
	require.Contains(t, out, `"b":2`)
}

func TestConcurrentLogging_NoRaces(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	filename := dir + "/concurrent.log"
	l, err := New(
		WithLevel(InfoLevel),
		WithOutput(FileOutput),
		WithFileOptions(FileOptions{Filename: filename}),
	)
	require.NoError(t, err)
	const goroutines = 50
	const perGoroutine = 200
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := range goroutines {
		go func(id int) {
			defer wg.Done()
			for j := range perGoroutine {
				l.Info("event", Fields{"goroutine": id, "iter": j})
			}
		}(i)
	}
	wg.Wait()
	require.NoError(t, l.Close())
	data, err := os.ReadFile(filename)
	require.NoError(t, err)
	got := strings.Count(string(data), `"message":"event"`)
	require.Equal(t, goroutines*perGoroutine, got, "every concurrent log should land in the file")
}

func TestClose_WaitsForInflight(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	filename := dir + "/inflight.log"
	l, err := New(
		WithLevel(InfoLevel),
		WithOutput(FileOutput),
		WithFileOptions(FileOptions{Filename: filename}),
	)
	require.NoError(t, err)
	const goroutines = 20
	const perGoroutine = 100
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for range goroutines {
		go func() {
			defer wg.Done()
			for range perGoroutine {
				l.Info("inflight")
			}
		}()
	}
	wg.Wait()
	require.NoError(t, l.Close())
	data, err := os.ReadFile(filename)
	require.NoError(t, err)
	got := strings.Count(string(data), "inflight")
	require.Equal(t, goroutines*perGoroutine, got)
}

func TestWithCallerSkip_AdjustsCallerFrame(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	filename := dir + "/skip.log"
	l, err := New(
		WithLevel(InfoLevel),
		WithOutput(FileOutput),
		WithFileOptions(FileOptions{Filename: filename}),
		WithCallerSkip(1),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = l.Close() })
	logViaWrapper(l, "wrapped")
	require.NoError(t, l.Close())
	data, err := os.ReadFile(filename)
	require.NoError(t, err)
	out := string(data)
	require.Contains(t, out, "wrapped")
	// With skip+1, caller should point to the test file (the caller of logViaWrapper),
	// not the wrapper helper itself
	require.Contains(t, out, "concurrency_test.go")
}

// logViaWrapper is a one-frame helper that logs via the given logger; used to verify WithCallerSkip
// shifts the reported caller to the wrapper's caller (the test) instead of this function
func logViaWrapper(l Logger, msg string) {
	l.Info(msg)
}

func TestWithWriter_UsesCustomWriter(t *testing.T) {
	t.Parallel()
	buf := &bytes.Buffer{}
	l, err := New(
		WithLevel(InfoLevel),
		WithWriter(buf),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = l.Close() })
	l.Info("custom")
	require.Contains(t, buf.String(), "custom")
}

// concurrentBuffer is a deliberately non-thread-safe writer used to verify WithSyncWriter serializes Writes
type concurrentBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (c *concurrentBuffer) Write(p []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.buf.Write(p)
}

func (c *concurrentBuffer) String() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.buf.String()
}

func TestWithSyncWriter_ConcurrentWritesSerialized(t *testing.T) {
	t.Parallel()
	cb := &concurrentBuffer{}
	l, err := New(
		WithLevel(InfoLevel),
		WithSyncWriter(cb),
	)
	require.NoError(t, err)
	const goroutines = 30
	const perGoroutine = 50
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for range goroutines {
		go func() {
			defer wg.Done()
			for range perGoroutine {
				l.Info("synced")
			}
		}()
	}
	wg.Wait()
	require.NoError(t, l.Close())
	got := strings.Count(cb.String(), "synced")
	require.Equal(t, goroutines*perGoroutine, got)
}

func TestFatalContext_AppliesExtractor(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	filename := dir + "/fatal-ctx.log"
	l, err := New(
		WithLevel(InfoLevel),
		WithOutput(FileOutput),
		WithFileOptions(FileOptions{Filename: filename}),
		WithExitFunc(func(_ int) {}),
		WithContextExtractor(func(ctx context.Context) Fields {
			if v, ok := ctx.Value(keyTrace).(string); ok {
				return Fields{"trace_id": v}
			}
			return nil
		}),
	)
	require.NoError(t, err)
	ctx := context.WithValue(context.Background(), keyTrace, "fatal-trace")
	l.FatalContext(ctx, "fatal with extractor")
	data, err := os.ReadFile(filename)
	require.NoError(t, err)
	out := string(data)
	require.Contains(t, out, `"trace_id":"fatal-trace"`)
	require.Contains(t, out, "fatal with extractor")
}

func TestWithWriter_NilIgnored(t *testing.T) {
	t.Parallel()
	buf := &bytes.Buffer{}
	l, err := New(
		WithLevel(InfoLevel),
		WithWriter(buf),
		WithWriter(nil),
	)
	require.NoError(t, err)
	l.Info("nil ignored")
	require.Contains(t, buf.String(), "nil ignored", "nil WithWriter should not clear a previously set writer")
}
