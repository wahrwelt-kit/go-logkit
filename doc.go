// Package logger provides a minimal structured logging interface backed by zerolog.
//
// It exposes a Logger interface with level-based methods (Debug, Info, Warn, Error, Fatal),
// contextual builders (WithFields, WithError), and functional options for construction.
// Output can be console, file, or both; file output uses lumberjack for rotation.
//
// Use New with Option functions to build a logger; use Noop for a silent logger in tests
// or when logging is disabled.
package logger
