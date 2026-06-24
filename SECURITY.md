# Security

## Trust Model

ReadMyPaper is a local desktop application. PDFs must be treated as untrusted input, and LLM endpoints must be treated as external services even when they run on the local machine.

## Implemented Controls

- `%PDF-` header validation before job creation;
- configurable limits for size, pages, workers, and queue depth;
- cryptographically random job IDs;
- PDF copy into a controlled directory before processing;
- atomic writes for text, metadata, models, and temporary audio;
- path validation before restoring or deleting artifacts;
- TTS bridge execution through `exec.CommandContext`, without a shell;
- text and options travel through temporary JSON, not command-line arguments;
- timeout, response limit, and redirect blocking in the LLM client;
- LLM fail-open policy to avoid silent content loss;
- TTS subprocess and job cancellation on shutdown.

## Data Sent Over The Network

- Piper: selected voice model and JSON download.
- Kokoro: downloads managed by the package/model hub used by the Kokoro runtime.
- LLM: only when enabled; blocks that survive the spatial stage are sent to the configured endpoint for classification and ordering.

There is no telemetry.

## Recommendations

- prefer an LLM endpoint on loopback or a trusted network;
- use HTTPS and a dedicated token when the endpoint is not on loopback;
- do not run the app with administrative privileges;
- keep Go, Fyne, Python, and TTS packages updated;
- process sensitive PDFs only in an environment with known cache and backup policies.

## Vulnerability Reporting

Do not publish sensitive documents, tokens, or private models in an issue. Send a minimal reproducible report to the repository maintainer, including version, operating system, impact, and reproduction steps without clinical or personal data.
