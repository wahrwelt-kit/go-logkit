package logkit

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestFieldsHelpers(t *testing.T) {
	t.Parallel()
	assert.Equal(t, Fields{"trace_id": "t1"}, TraceID("t1"))
	assert.Equal(t, Fields{"request_id": "r1"}, RequestID("r1"))
	assert.Equal(t, Fields{"user_id": "u1"}, UserID("u1"))
	assert.Equal(t, Fields{"duration": "1s"}, Duration(time.Second))
	assert.Equal(t, Fields{"duration": int64(1500)}, DurationMs(1500*time.Millisecond))
	assert.Equal(t, Fields{"component": "http"}, Component("http"))
	assert.Nil(t, Error(nil))
	assert.Equal(t, Fields{"error": errors.New("e")}, Error(errors.New("e")))
}
