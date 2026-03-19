package logkit

import "time"

const (
	keyTraceID   = "trace_id"
	keyRequestID = "request_id"
	keyUserID    = "user_id"
	keyError     = "error"
	keyDuration  = "duration"
	keyComponent = "component"
)

// TraceID returns Fields with the trace_id key for distributed tracing. Attach to log events
// when a trace ID is available (e.g. from OpenTelemetry or incoming request headers).
func TraceID(id string) Fields {
	return Fields{keyTraceID: id}
}

// RequestID returns Fields with the request_id key. Use the same ID as in HTTP headers (e.g. X-Request-ID)
// so logs can be correlated with a single request across services.
func RequestID(id string) Fields {
	return Fields{keyRequestID: id}
}

// UserID returns Fields with the user_id key. Prefer opaque IDs; do not log unredacted PII or tokens.
func UserID(id string) Fields {
	return Fields{keyUserID: id}
}

// Error returns Fields with the error key for structured error logging. Returns nil if err is nil—
// in that case no field is added, so callers can pass the result directly to log methods.
func Error(err error) Fields {
	if err == nil {
		return nil
	}
	return Fields{keyError: err}
}

// Duration returns Fields with the duration key formatted as a string (e.g. "1.5s"). Prefer DurationMs
// when logs are ingested by structured backends (ELK, Loki, ClickHouse) for querying and aggregation.
func Duration(d time.Duration) Fields {
	return Fields{keyDuration: d.String()}
}

// DurationMs returns Fields with the duration key as int64 milliseconds. Use for structured backends
// (ELK, Loki, ClickHouse) so duration can be queried and aggregated; for human-readable logs use Duration.
func DurationMs(d time.Duration) Fields {
	return Fields{keyDuration: d.Milliseconds()}
}

// Component returns Fields with the component key. Use to tag events by handler name, service layer,
// or module so logs can be filtered by component in centralized logging.
func Component(name string) Fields {
	return Fields{keyComponent: name}
}
