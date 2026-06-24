# Self-Contained Packaging Plan — ReadMyPaper-go

## Goal

Ship `ReadMyPaper-go` as a macOS `.app` that opens with a double click and does not depend on Python, environment variables, or a terminal. The bundle must contain the Go binary, the Fyne interface, the Python runtime, and the packages required by the TTS backends (Piper and Kokoro).

## Current State

- The app core is Go/Fyne.
- The Python scripts for the TTS backends are embedded in the binary through `go:embed` and extracted to cache on first use.
- At runtime, the app runs those scripts with an external Python interpreter configured by `READMYPAPER_PYTHON_BIN`.
- The virtual environment must be created manually and activated before running the app.
- `requirements-tts.txt` lists the Python dependencies.
- The `fyne package` target exists in the `Makefile`, but it generates only the Go binary with metadata; it does not include embedded Python.

## Architecture Decisions

### 1. Relocatable Python

Use **Python standalone** from `astral-sh/python-build-standalone` (continuation of `indygreg/python-build-standalone`) as the base. This distribution is already compiled to be relocatable on macOS and works when copied inside a `.app`.

Packages to install in the bundled Python:

- `piper-tts`
- `kokoro-onnx` (or the equivalent used by the project)
- any other dependencies listed in `requirements-tts.txt`

Install directly into the relocatable Python, without creating an internal `venv`, to reduce complexity and absolute paths.

The `scripts/package-macos.sh` script uses CPython 3.12 by default and allows the source to be changed with `PYTHON_STANDALONE_REPO`, `PYTHON_VERSION_PREFIX`, or explicit architecture-specific URLs.

### 2. `.app` Structure

```text
ReadMyPaper.app/
├── Contents/
│   ├── Info.plist
│   ├── MacOS/
│   │   └── readmypaper
│   └── Resources/
│       ├── Icon.png
│       └── python/
│           ├── bin/python3
│           └── lib/python3.12/site-packages/
│               ├── piper/
│               ├── kokoro/
│               └── ...
```

### 3. Runtime Resource Discovery

Add a Go helper that starts from `os.Executable()`, detects whether the binary is inside a `.app`, and resolves the relative path to `Contents/Resources/python/bin/python3`.

Interpreter resolution order:

1. `READMYPAPER_PYTHON_BIN` when defined (development mode).
2. Bundled Python at `Contents/Resources/python/bin/python3`.
3. Architecture-specific bundled Python at `Contents/Resources/python-arm64/bin/python3` or `Contents/Resources/python-x86_64/bin/python3`, used by the universal package.
4. `python3` from `PATH` (fallback for development machines).

### 4. Voice And Model Cache

TTS voices and models continue to be downloaded on first run and stored in `~/Library/Caches/ReadMyPaper`. This keeps the bundle smaller and allows models to be updated without rebuilding.

## Required Changes

### Go Code

- `internal/config/config.go`
  - Add a helper to resolve the bundled Python path.
  - Preserve `READMYPAPER_PYTHON_BIN` handling.

- `internal/tts/tts.go`
  - Adjust interpreter detection to use the resolver.

- `internal/tts/piper.go` and `internal/tts/kokoro.go`
  - Ensure the materialized Python bridges run with the bundled Python.

### Build And Scripts

- Create `scripts/package-macos.sh`.
  - Download Python standalone for the target architecture (arm64, x86_64, or universal).
  - Install dependencies from `requirements-tts.txt`.
  - Compile `cmd/readmypaper` with `go build`.
  - Assemble the `.app` structure.
  - Generate `Info.plist`.
  - Copy the icon from `assets/Icon.png`.
  - Optional: sign with `codesign` and notarize with `notarytool` if a developer certificate is available.

- Update `Makefile`.
  - Add the `package-macos` target.
  - Keep Fyne's `package` target as a lightweight alternative.

## Execution Phases

### Phase 1 — Code Analysis

- Map every place where `READMYPAPER_PYTHON_BIN` and `python3` are used.
- Confirm which Python packages are in `requirements-tts.txt`.
- Check whether Kokoro requires additional binaries or only Python packages.

### Phase 2 — Relocatable Python Prototype

- Download `python-build-standalone` for macOS.
- Install `requirements-tts.txt`.
- Run the Python bridges manually to validate Piper and Kokoro.
- Measure Python size after installation.

### Phase 3 — Go Resource Detection

- Implement the bundled Python path resolver.
- Add unit tests for these scenarios:
  - inside the `.app`
  - outside the `.app`
  - with the environment variable defined

### Phase 4 — Packaging Script

- Create `scripts/package-macos.sh`.
- Support arm64 and x86_64 architectures.
- Generate `Info.plist` with bundle ID `io.github.thalesmms.readmypaper` as defined in `FyneApp.toml`.
- Verify that the icon exists in `assets/Icon.png`.

### Phase 5 — Tests

- Run the `.app` on a machine without Python in `PATH`.
- Process the sample PDF.
- Confirm that `reading.wav` is generated.
- Test Piper and Kokoro.
- Test fallback when `READMYPAPER_PYTHON_BIN` is defined.

### Phase 6 — Documentation

- Update `README.md` with `.app` build instructions.
- Record bundle size, first-run time, and notarization limitations.

## Expected Bundle Size

- Arm64 build validated on 2026-06-24: `dist/ReadMyPaper.app` was about 854 MB.
- The largest cost comes from Kokoro and its current dependencies, including Torch, spaCy, Transformers, and native wheels.
- Piper-only builds would be substantially smaller, but they do not satisfy the goal of this self-contained package with both Piper and Kokoro.

## Risks And Limitations

- **Notarization**: without an Apple Developer ID, the user needs to authorize the app in **Security & Privacy**.
- **First run**: it can still be slow because voices and models must be downloaded.
- **Size**: Kokoro and its models significantly increase the bundle when included.
- **Updates**: changes in Python packages require a new build.

## Recommended Next Steps

1. Start with Phase 2 (relocatable Python prototype).
2. After validation, apply Phase 3 (Go resource detection) and Phase 4 (packaging script).
