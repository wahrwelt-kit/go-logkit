package logkit

import (
	"bytes"
	"sync"
)

const (
	testFieldValue  = "value"
	testSecretValue = "super-secret-value"
	testPayloadKey  = "payload"
)

type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}
