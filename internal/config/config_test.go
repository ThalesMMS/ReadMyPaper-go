package config

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestLoadUsesOverridesAndNormalizesLimits(t *testing.T) {
	root := t.TempDir()
	t.Setenv("READMYPAPER_DATA_DIR", filepath.Join(root, "data"))
	t.Setenv("READMYPAPER_CACHE_DIR", filepath.Join(root, "cache"))
	t.Setenv("READMYPAPER_MAX_WORKERS", "0")
	t.Setenv("READMYPAPER_MAX_PENDING_JOBS", "-4")
	t.Setenv("READMYPAPER_JOB_RETENTION_HOURS", "-2")
	t.Setenv("READMYPAPER_MAX_UPLOAD_BYTES", "8589934592")
	t.Setenv("READMYPAPER_LLM_ENABLED", "yes")
	t.Setenv("READMYPAPER_PYTHON_BIN", "/custom/python")

	settings, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if settings.MaxWorkers != 1 || settings.MaxPendingJobs != 1 {
		t.Fatalf("worker limits were not normalized: %#v", settings)
	}
	if settings.JobRetentionHours != 0 {
		t.Fatalf("negative retention must disable TTL, got %d", settings.JobRetentionHours)
	}
	if settings.MaxUploadBytes != 8589934592 {
		t.Fatalf("64-bit upload limit was not parsed: %d", settings.MaxUploadBytes)
	}
	if !settings.LLMEnabled || settings.PythonBinary != "/custom/python" {
		t.Fatalf("environment overrides were not loaded: %#v", settings)
	}
}

func TestLoadRejectsNonPositiveStructuralLimits(t *testing.T) {
	root := t.TempDir()
	t.Setenv("READMYPAPER_DATA_DIR", filepath.Join(root, "data"))
	t.Setenv("READMYPAPER_CACHE_DIR", filepath.Join(root, "cache"))
	t.Setenv("READMYPAPER_MAX_UPLOAD_BYTES", "-1")

	if _, err := Load(); err == nil {
		t.Fatal("expected a non-positive upload limit to be rejected")
	}
}

func TestResolvePythonBinaryUsesEnvironmentBeforeBundle(t *testing.T) {
	executable, _ := createBundlePython(t)

	got := resolvePythonBinaryForExecutable(executable, func(name string) string {
		if name == "READMYPAPER_PYTHON_BIN" {
			return " /custom/python "
		}
		return ""
	})

	if got != "/custom/python" {
		t.Fatalf("environment override was not preferred: got %q", got)
	}
}

func TestResolvePythonBinaryUsesBundledPythonInsideApp(t *testing.T) {
	executable, bundledPython := createBundlePython(t)

	got := resolvePythonBinaryForExecutable(executable, func(string) string { return "" })

	if got != bundledPython {
		t.Fatalf("bundled Python was not resolved: got %q, want %q", got, bundledPython)
	}
}

func TestResolvePythonBinaryUsesArchSpecificBundledPython(t *testing.T) {
	executable, bundledPython := createArchSpecificBundlePython(t)

	got := resolvePythonBinaryForExecutable(executable, func(string) string { return "" })

	if got != bundledPython {
		t.Fatalf("arch-specific bundled Python was not resolved: got %q, want %q", got, bundledPython)
	}
}

func TestResolvePythonBinaryFallsBackEmptyOutsideApp(t *testing.T) {
	executable := filepath.Join(t.TempDir(), "readmypaper")
	if err := os.WriteFile(executable, []byte("binary"), 0o755); err != nil {
		t.Fatal(err)
	}

	got := resolvePythonBinaryForExecutable(executable, func(string) string { return "" })

	if got != "" {
		t.Fatalf("expected no configured Python outside an app bundle, got %q", got)
	}
}

func createBundlePython(t *testing.T) (string, string) {
	t.Helper()
	root := filepath.Join(t.TempDir(), "ReadMyPaper.app", "Contents")
	executable := filepath.Join(root, "MacOS", "readmypaper")
	python := filepath.Join(root, "Resources", "python", "bin", "python3")
	for _, directory := range []string{filepath.Dir(executable), filepath.Dir(python)} {
		if err := os.MkdirAll(directory, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(executable, []byte("binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(python, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return executable, python
}

func createArchSpecificBundlePython(t *testing.T) (string, string) {
	t.Helper()
	root := filepath.Join(t.TempDir(), "ReadMyPaper.app", "Contents")
	executable := filepath.Join(root, "MacOS", "readmypaper")
	python := filepath.Join(root, "Resources", "python-"+macOSPythonArchName(), "bin", "python3")
	for _, directory := range []string{filepath.Dir(executable), filepath.Dir(python)} {
		if err := os.MkdirAll(directory, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(executable, []byte("binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(python, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return executable, python
}

func macOSPythonArchName() string {
	if runtime.GOARCH == "amd64" {
		return "x86_64"
	}
	return runtime.GOARCH
}
