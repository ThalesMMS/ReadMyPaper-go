package config

import (
	"path/filepath"
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
