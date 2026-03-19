package logkit

import (
	"context"
	"log/slog"
)

// SlogHandler returns a slog.Handler that forwards log/slog records to the given Logger.
// Use slog.New(SlogHandler(l)) so code that expects slog.Logger or slog.Handler can use a logkit Logger.
// WithGroup and WithAttrs are supported; group names are prefixed to attribute keys (e.g. group "http", attr "method" → "http.method").
// The adapter does not filter by level; Enabled always returns true—level filtering is done by the Logger.
func SlogHandler(l Logger) slog.Handler {
	if l == nil {
		l = noopLogger{}
	}
	return &slogAdapter{logger: l}
}

type slogAdapter struct {
	logger Logger
	attrs  Fields
	group  string
}

func (a *slogAdapter) Enabled(_ context.Context, _ slog.Level) bool {
	return true
}

func (a *slogAdapter) Handle(_ context.Context, r slog.Record) error {
	fields := make(Fields, len(a.attrs)+r.NumAttrs())
	prefix := a.group
	if prefix != "" {
		prefix += "."
	}
	for k, v := range a.attrs {
		fields[k] = v
	}
	r.Attrs(func(attr slog.Attr) bool {
		key := prefix + attr.Key
		fields[key] = attr.Value.Any()
		return true
	})

	msg := r.Message
	switch {
	case r.Level >= slog.LevelError:
		a.logger.Error(msg, fields)
	case r.Level >= slog.LevelWarn:
		a.logger.Warn(msg, fields)
	case r.Level >= slog.LevelInfo:
		a.logger.Info(msg, fields)
	default:
		a.logger.Debug(msg, fields)
	}
	return nil
}

func (a *slogAdapter) WithAttrs(attrs []slog.Attr) slog.Handler {
	f := make(Fields, len(a.attrs)+len(attrs))
	for k, v := range a.attrs {
		f[k] = v
	}
	for _, attr := range attrs {
		key := attr.Key
		if a.group != "" {
			key = a.group + "." + key
		}
		f[key] = attr.Value.Any()
	}
	return &slogAdapter{logger: a.logger, attrs: f, group: a.group}
}

func (a *slogAdapter) WithGroup(name string) slog.Handler {
	g := name
	if a.group != "" {
		g = a.group + "." + name
	}
	return &slogAdapter{logger: a.logger, attrs: a.attrs, group: g}
}
