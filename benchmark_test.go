package logkit

import (
	"errors"
	"io"
	"testing"
	"time"
)

func benchLogger(b *testing.B, opts ...Option) Logger {
	b.Helper()
	l, err := New(opts...)
	if err != nil {
		b.Fatal(err)
	}
	zl := l.(*zerologLogger)
	zl.zl = zl.zl.Output(io.Discard)
	return zl
}

func BenchmarkInfoNoFields(b *testing.B) {
	l := benchLogger(b, WithLevel(InfoLevel), WithOutput(ConsoleOutput))
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		l.Info("benchmark message")
	}
}

func BenchmarkInfoWithFields(b *testing.B) {
	l := benchLogger(b, WithLevel(InfoLevel), WithOutput(ConsoleOutput))
	f := Fields{"key": "value", "count": 42}
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
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
	for i := 0; i < b.N; i++ {
		sanitizeFields(f)
	}
}
