package logkit

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIntoContext_FromContext(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	assert.NotNil(t, FromContext(ctx))
	assert.Equal(t, Noop(), FromContext(ctx))

	l, err := New(WithLevel(InfoLevel), WithOutput(ConsoleOutput))
	require.NoError(t, err)
	ctx = IntoContext(ctx, l)
	assert.Same(t, l, FromContext(ctx))
}

func TestFromContext_NilLogger(t *testing.T) {
	t.Parallel()
	var nilLogger Logger
	ctx := context.WithValue(context.Background(), contextKey{}, nilLogger)
	assert.Equal(t, Noop(), FromContext(ctx))
}

func TestIntoContext_NilContext_Panics(t *testing.T) {
	l, err := New(WithLevel(InfoLevel), WithOutput(ConsoleOutput))
	require.NoError(t, err)
	// SA1012: nil is intentional to verify IntoContext panics on nil context
	require.Panics(t, func() { IntoContext(nil, l) }) //nolint:staticcheck
}
