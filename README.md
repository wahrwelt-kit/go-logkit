# go-logkit

Structured logging for Go (zerolog-backed Logger interface, levels, options, file/console).

## Install

```bash
go get github.com/TakuyaYagam1/go-logkit
```

## Package

- **`logger`** — Logger interface (Debug, Info, Warn, Error, Fatal; WithFields, WithError). `New(opts...)` with WithLevel, WithOutput (Console/File/Both), WithFileOptions; file output requires non-empty Filename (ErrEmptyFilename otherwise). `Noop()` for tests or when logging is disabled. Merges variadic Fields per call.

Requires **github.com/rs/zerolog** and **gopkg.in/natefinch/lumberjack.v2**.
