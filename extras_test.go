package logkit

import (
	"bytes"
	"errors"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	pkgerrors "github.com/pkg/errors"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
)

// hostnameHook adds a fixed hostname field to every event; used to verify Hook integration
type hostnameHook struct{ name string }

func (h hostnameHook) Run(e *zerolog.Event, _ zerolog.Level, _ string) {
	e.Str("hostname", h.name)
}

// levelCounterHook counts events per level for assertion in tests
type levelCounterHook struct {
	mu     sync.Mutex
	counts map[zerolog.Level]int
}

func (h *levelCounterHook) Run(_ *zerolog.Event, level zerolog.Level, _ string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.counts == nil {
		h.counts = make(map[zerolog.Level]int)
	}
	h.counts[level]++
}

func (h *levelCounterHook) get(level zerolog.Level) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.counts[level]
}

func TestWithHooks_AddsField(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	filename := dir + "/hook.log"
	l, err := New(
		WithLevel(InfoLevel),
		WithOutput(FileOutput),
		WithFileOptions(FileOptions{Filename: filename}),
		WithHooks(hostnameHook{name: "test-host"}),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = l.Close() })
	l.Info("hooked")
	require.NoError(t, l.Close())
	data, err := os.ReadFile(filename)
	require.NoError(t, err)
	require.Contains(t, string(data), `"hostname":"test-host"`)
	require.Contains(t, string(data), "hooked")
}

func TestWithHooks_NilHookSkipped(t *testing.T) {
	t.Parallel()
	l, err := New(WithLevel(InfoLevel), WithOutput(ConsoleOutput), WithHooks(nil, hostnameHook{name: "h"}))
	require.NoError(t, err)
	require.NotNil(t, l)
	l.Info("ok")
}

func TestWithHooks_MultipleRunsInOrder(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	filename := dir + "/multi-hook.log"
	counter := &levelCounterHook{}
	l, err := New(
		WithLevel(DebugLevel),
		WithOutput(FileOutput),
		WithFileOptions(FileOptions{Filename: filename}),
		WithHooks(counter, hostnameHook{name: "n"}),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = l.Close() })
	l.Debug("d")
	l.Info("i")
	l.Warn("w")
	l.Error("e")
	require.Equal(t, 1, counter.get(zerolog.DebugLevel))
	require.Equal(t, 1, counter.get(zerolog.InfoLevel))
	require.Equal(t, 1, counter.get(zerolog.WarnLevel))
	require.Equal(t, 1, counter.get(zerolog.ErrorLevel))
}

func TestWithSampling_BasicSamplerKeepsEveryNth(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	filename := dir + "/sample.log"
	const n = 10
	const total = 100
	l, err := New(
		WithLevel(InfoLevel),
		WithOutput(FileOutput),
		WithFileOptions(FileOptions{Filename: filename}),
		WithSampling(&zerolog.BasicSampler{N: n}),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = l.Close() })
	for range total {
		l.Info("sampled")
	}
	require.NoError(t, l.Close())
	data, err := os.ReadFile(filename)
	require.NoError(t, err)
	got := strings.Count(string(data), "sampled")
	require.Equal(t, total/n, got, "BasicSampler{N:%d} should keep total/N events out of %d", n, total)
}

func TestWithSampling_NilDisabled(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	filename := dir + "/no-sample.log"
	l, err := New(
		WithLevel(InfoLevel),
		WithOutput(FileOutput),
		WithFileOptions(FileOptions{Filename: filename}),
		WithSampling(nil),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = l.Close() })
	for range 5 {
		l.Info("none")
	}
	require.NoError(t, l.Close())
	data, err := os.ReadFile(filename)
	require.NoError(t, err)
	require.Equal(t, 5, strings.Count(string(data), "none"))
}

func TestWithStackTrace_AddsStackForPkgErrors(t *testing.T) {
	dir := t.TempDir()
	filename := dir + "/stack.log"
	l, err := New(
		WithLevel(InfoLevel),
		WithOutput(FileOutput),
		WithFileOptions(FileOptions{Filename: filename}),
		WithStackTrace(),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = l.Close() })
	wrapped := pkgerrors.WithStack(errors.New("base"))
	l.WithError(wrapped).Error("boom")
	require.NoError(t, l.Close())
	data, err := os.ReadFile(filename)
	require.NoError(t, err)
	out := string(data)
	require.Contains(t, out, `"error":"base"`)
	require.Contains(t, out, `"stack":`)
}

func TestWithStackTrace_NoStackForPlainError(t *testing.T) {
	dir := t.TempDir()
	filename := dir + "/no-stack.log"
	l, err := New(
		WithLevel(InfoLevel),
		WithOutput(FileOutput),
		WithFileOptions(FileOptions{Filename: filename}),
		WithStackTrace(),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = l.Close() })
	l.WithError(errors.New("plain")).Error("boom")
	require.NoError(t, l.Close())
	data, err := os.ReadFile(filename)
	require.NoError(t, err)
	out := string(data)
	require.Contains(t, out, `"error":"plain"`)
	require.NotContains(t, out, `"stack":`)
}

func TestWithAsync_FlushesPendingEventsOnClose(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	filename := dir + "/async.log"
	l, err := New(
		WithLevel(InfoLevel),
		WithOutput(FileOutput),
		WithFileOptions(FileOptions{Filename: filename}),
		WithAsync(AsyncOptions{Size: 1024, PollInterval: 5 * time.Millisecond}),
	)
	require.NoError(t, err)
	for range 50 {
		l.Info("async event")
	}
	require.NoError(t, l.Close())
	data, err := os.ReadFile(filename)
	require.NoError(t, err)
	require.GreaterOrEqual(t, strings.Count(string(data), "async event"), 1, "Close should flush pending diode events")
}

func TestWithAsync_DropsUnderPressure(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	filename := dir + "/async-drop.log"
	dropped := atomic.Int64{}
	l, err := New(
		WithLevel(InfoLevel),
		WithOutput(FileOutput),
		WithFileOptions(FileOptions{Filename: filename}),
		WithAsync(AsyncOptions{
			Size:         8,
			PollInterval: 100 * time.Millisecond,
			OnDrop:       func(missed int) { dropped.Add(int64(missed)) },
		}),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = l.Close() })
	for range 5000 {
		l.Info("flood")
	}
	require.NoError(t, l.Close())
	require.Positive(t, dropped.Load(), "OnDrop should be called when producer outruns the diode buffer")
}

// closeCounter wraps a bytes.Buffer and tracks how many times Close is called.
// Used to verify that the close-chain does not double-close the underlying writer.
type closeCounter struct {
	bytes.Buffer

	closed atomic.Int32
}

func (c *closeCounter) Close() error {
	c.closed.Add(1)
	return nil
}

func TestWithAsync_SingleCloseOnCustomWriter(t *testing.T) {
	t.Parallel()
	cc := &closeCounter{}
	l, err := New(
		WithLevel(InfoLevel),
		WithWriter(cc),
		WithAsync(AsyncOptions{Size: 256, PollInterval: 5 * time.Millisecond}),
	)
	require.NoError(t, err)
	l.Info("async custom writer")
	require.NoError(t, l.Close())
	require.Equal(t, int32(1), cc.closed.Load(), "custom writer Close must be called exactly once even with WithAsync")
}
