package logkit

import (
	"context"
	"log/slog"
	"maps"
)

// SlogHandler returns a slog.Handler that forwards log/slog records to the given Logger
// Use slog.New(SlogHandler(l)) so code that expects slog.Logger or slog.Handler can use a logkit Logger
// WithGroup and WithAttrs are supported; group names are prefixed to attribute keys (e.g. group "http", attr "method" -> "http.method")
// slog.Group inline attrs in records or WithAttrs calls are recursively expanded with their key as prefix
// If the Logger implements Leveler, Enabled returns false for levels that would be dropped, avoiding
// unnecessary slog.Record construction. For loggers created by New, caller is taken from slog.Record.PC
// so slog output points to the original slog call site.
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

func (a *slogAdapter) Enabled(_ context.Context, level slog.Level) bool {
	lev, ok := a.logger.(Leveler)
	if !ok {
		return true
	}
	return slogToLogkitLevel(level) >= lev.Level()
}

func (a *slogAdapter) Handle(ctx context.Context, r slog.Record) error {
	fields := make(Fields, len(a.attrs)+r.NumAttrs())
	prefix := a.group
	if prefix != "" {
		prefix += "."
	}
	maps.Copy(fields, a.attrs)
	r.Attrs(func(attr slog.Attr) bool {
		expandAttr(fields, prefix, attr)
		return true
	})

	if zl, ok := a.logger.(*zerologLogger); ok && !isNilInterface(zl) {
		zl.logSlogRecord(ctx, r, fields)
		return nil
	}

	msg := r.Message
	switch {
	case r.Level >= slog.LevelError:
		a.logger.ErrorContext(ctx, msg, fields)
	case r.Level >= slog.LevelWarn:
		a.logger.WarnContext(ctx, msg, fields)
	case r.Level >= slog.LevelInfo:
		a.logger.InfoContext(ctx, msg, fields)
	default:
		a.logger.DebugContext(ctx, msg, fields)
	}
	return nil
}

func (l *zerologLogger) logSlogRecord(ctx context.Context, r slog.Record, fields Fields) {
	if !l.state.begin() {
		return
	}
	defer l.state.end()

	allFields := l.applyExtractors(ctx, []Fields{fields})
	caller := callerFromPC(r.PC)
	msg := r.Message
	switch {
	case r.Level >= slog.LevelError:
		l.logUncheckedWithCaller(l.zl.Error(), msg, caller, allFields...) //nolint:zerologlint // event consumed by logUncheckedWithCaller
	case r.Level >= slog.LevelWarn:
		l.logUncheckedWithCaller(l.zl.Warn(), msg, caller, allFields...) //nolint:zerologlint // event consumed by logUncheckedWithCaller
	case r.Level >= slog.LevelInfo:
		l.logUncheckedWithCaller(l.zl.Info(), msg, caller, allFields...) //nolint:zerologlint // event consumed by logUncheckedWithCaller
	default:
		l.logUncheckedWithCaller(l.zl.Debug(), msg, caller, allFields...) //nolint:zerologlint // event consumed by logUncheckedWithCaller
	}
}

func (a *slogAdapter) WithAttrs(attrs []slog.Attr) slog.Handler {
	f := make(Fields, len(a.attrs)+len(attrs))
	maps.Copy(f, a.attrs)
	prefix := a.group
	if prefix != "" {
		prefix += "."
	}
	for _, attr := range attrs {
		expandAttr(f, prefix, attr)
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

// expandAttr recursively expands attr into fields using prefix as the key prefix
// slog.Group attrs are expanded by nesting sub-attrs under the group key; an empty
// group key inlines sub-attrs directly under the current prefix
func expandAttr(fields Fields, prefix string, attr slog.Attr) {
	if attr.Value.Kind() == slog.KindGroup {
		sub := attr.Value.Group()
		if len(sub) == 0 {
			return
		}
		groupPrefix := prefix
		if attr.Key != "" {
			groupPrefix += attr.Key + "."
		}
		for _, a := range sub {
			expandAttr(fields, groupPrefix, a)
		}
		return
	}
	if attr.Key == "" {
		return
	}
	fields[prefix+attr.Key] = attr.Value.Any()
}

// slogToLogkitLevel maps a slog.Level to the equivalent logkit Level
func slogToLogkitLevel(level slog.Level) Level {
	switch {
	case level >= slog.LevelError:
		return ErrorLevel
	case level >= slog.LevelWarn:
		return WarnLevel
	case level >= slog.LevelInfo:
		return InfoLevel
	default:
		return DebugLevel
	}
}
