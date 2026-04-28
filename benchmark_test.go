package logkit

import (
	"errors"
	"io"
	"testing"
	"time"

	"github.com/rs/zerolog"
)

func benchLogger(b *testing.B, opts ...Option) Logger {
	b.Helper()
	l, err := New(opts...)
	if err != nil {
		b.Fatal(err)
	}
	zl := l.(*zerologLogger) //nolint:revive // internal type known at benchmark setup
	zl.zl = zl.zl.Output(io.Discard)
	return zl
}

func BenchmarkInfoNoFields(b *testing.B) {
	l := benchLogger(b, WithLevel(InfoLevel), WithOutput(ConsoleOutput))
	b.ResetTimer()
	b.ReportAllocs()
	for range b.N {
		l.Info("benchmark message")
	}
}

func BenchmarkInfoWithFields(b *testing.B) {
	l := benchLogger(b, WithLevel(InfoLevel), WithOutput(ConsoleOutput))
	f := Fields{"key": "value", "count": 42}
	b.ResetTimer()
	b.ReportAllocs()
	for range b.N {
		l.Info("benchmark message", f)
	}
}

func BenchmarkSanitizeFields(b *testing.B) {
	f := Fields{
		"string":   "hello\nworld",
		"int":      42,
		"float":    3.14,
		"error":    errors.New("test\nerror"),
		"stringer": time.Second,
	}
	b.ResetTimer()
	b.ReportAllocs()
	for range b.N {
		sanitizeFields(f)
	}
}

func BenchmarkInfo_WithSampling(b *testing.B) {
	l := benchLogger(b,
		WithLevel(InfoLevel),
		WithOutput(ConsoleOutput),
		WithSampling(&zerolog.BasicSampler{N: 10}),
	)
	b.ResetTimer()
	b.ReportAllocs()
	for range b.N {
		l.Info("sampled benchmark")
	}
}

type benchHook struct{}

func (benchHook) Run(e *zerolog.Event, _ zerolog.Level, _ string) {
	e.Str("hostname", "bench-host")
}

func BenchmarkInfo_WithHook(b *testing.B) {
	l := benchLogger(b,
		WithLevel(InfoLevel),
		WithOutput(ConsoleOutput),
		WithHooks(benchHook{}),
	)
	b.ResetTimer()
	b.ReportAllocs()
	for range b.N {
		l.Info("hooked benchmark")
	}
}

// BenchmarkInfo_Parallel measures the hot path under contention from multiple goroutines
// This is the case the atomic.Bool + WaitGroup design optimizes for - the prior RWMutex would
// cache-line bounce on every RLock; atomic operations split state and reduce cross-CPU traffic
func BenchmarkInfo_Parallel(b *testing.B) {
	l := benchLogger(b, WithLevel(InfoLevel), WithOutput(ConsoleOutput))
	b.ResetTimer()
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			l.Info("parallel")
		}
	})
}
