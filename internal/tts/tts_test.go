package tts

import (
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
)

func TestResolvePythonPreservesConfiguredPathWithSpaces(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test fixture is a Unix executable script")
	}
	directory := filepath.Join(t.TempDir(), "directory with spaces")
	if err := os.MkdirAll(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	executable := filepath.Join(directory, "python with spaces")
	if err := os.WriteFile(executable, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	binary, args, err := resolvePython(executable)
	if err != nil {
		t.Fatal(err)
	}
	if binary != executable {
		t.Fatalf("configured path was changed: got %q, want %q", binary, executable)
	}
	if len(args) != 0 {
		t.Fatalf("unexpected prefix args: %#v", args)
	}
}

func TestMaterializeBridgeIsSafeUnderConcurrency(t *testing.T) {
	runner := bridgeRunner{ModelsDir: t.TempDir()}
	const workers = 12
	var wait sync.WaitGroup
	errorsFound := make(chan error, workers)
	paths := make(chan string, workers)
	for range workers {
		wait.Add(1)
		go func() {
			defer wait.Done()
			path, err := runner.materializeScript("piper_bridge.py")
			if err != nil {
				errorsFound <- err
				return
			}
			paths <- path
		}()
	}
	wait.Wait()
	close(errorsFound)
	close(paths)
	for err := range errorsFound {
		t.Fatal(err)
	}
	var expected string
	for path := range paths {
		if expected == "" {
			expected = path
		}
		if path != expected {
			t.Fatalf("materialized paths differ: %q and %q", expected, path)
		}
	}
	contents, err := os.ReadFile(expected)
	if err != nil {
		t.Fatal(err)
	}
	if len(contents) == 0 {
		t.Fatal("materialized bridge is empty")
	}
}
