# Contributing

## Environment

1. Install Go and Fyne's native dependencies.
2. Run `go mod download`.
3. Optionally configure the Python environment described in the README for manual TTS tests.

## Before Sending Changes

```bash
gofmt -w .
go test ./...
go vet ./...
```

On a headless runner:

```bash
go test -tags ci ./...
```

## Guidelines

- preserve the boundaries between UI, domain, pipeline, and adapters;
- do not block the Fyne thread with extraction, network, or TTS work;
- use small interfaces for heavy integrations;
- write final files atomically;
- keep LLM failures fail-open;
- include regression tests for changes in cleaning or layout heuristics;
- do not add telemetry calls.
