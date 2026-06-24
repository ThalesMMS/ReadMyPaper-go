package tts

import (
	"bufio"
	"bytes"
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"github.com/ThalesMMS/ReadMyPaper-go/internal/cleaner"
	"github.com/ThalesMMS/ReadMyPaper-go/internal/domain"
	"github.com/ThalesMMS/ReadMyPaper-go/internal/util"
)

// ProgressCallback receives a normalized ratio and a human-readable step.
type ProgressCallback func(float64, string)

// Engine is implemented by each local synthesis backend.
type Engine interface {
	Name() string
	Synthesize(context.Context, string, string, domain.ProcessingOptions, VoiceSpec, ProgressCallback) (VoiceSpec, error)
}

//go:embed bridge/*.py
var bridgeFiles embed.FS

var bridgeMaterializeMu sync.Mutex

type bridgeRunner struct {
	PythonBinary string
	ModelsDir    string
}

type bridgeProgress struct {
	Progress float64 `json:"progress"`
	Step     string  `json:"step"`
}

func (r bridgeRunner) run(ctx context.Context, scriptName string, request any, progress ProgressCallback) error {
	python, prefixArgs, err := resolvePython(r.PythonBinary)
	if err != nil {
		return err
	}
	scriptPath, err := r.materializeScript(scriptName)
	if err != nil {
		return err
	}
	requestDir := filepath.Join(r.ModelsDir, "requests")
	if err := os.MkdirAll(requestDir, 0o755); err != nil {
		return fmt.Errorf("create bridge request directory: %w", err)
	}
	requestFile, err := os.CreateTemp(requestDir, "request-*.json")
	if err != nil {
		return err
	}
	requestPath := requestFile.Name()
	defer os.Remove(requestPath)
	encoder := json.NewEncoder(requestFile)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(request); err != nil {
		_ = requestFile.Close()
		return fmt.Errorf("encode TTS request: %w", err)
	}
	if err := requestFile.Close(); err != nil {
		return err
	}

	args := append(append([]string{}, prefixArgs...), scriptPath, "--request", requestPath)
	command := exec.CommandContext(ctx, python, args...)
	cacheDir := filepath.Join(r.ModelsDir, "huggingface")
	_ = os.MkdirAll(cacheDir, 0o755)
	command.Env = append(os.Environ(),
		"PYTHONUNBUFFERED=1",
		"HF_HOME="+cacheDir,
		"TRANSFORMERS_CACHE="+filepath.Join(cacheDir, "transformers"),
	)
	stdout, err := command.StdoutPipe()
	if err != nil {
		return err
	}
	stderr, err := command.StderrPipe()
	if err != nil {
		return err
	}
	if err := command.Start(); err != nil {
		return fmt.Errorf("start Python TTS bridge: %w", err)
	}

	var stderrTail boundedBuffer
	var wait sync.WaitGroup
	wait.Add(2)
	go func() {
		defer wait.Done()
		scanBridgeOutput(stdout, progress, nil)
	}()
	go func() {
		defer wait.Done()
		scanBridgeOutput(stderr, nil, &stderrTail)
	}()
	waitErr := command.Wait()
	wait.Wait()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if waitErr != nil {
		detail := strings.TrimSpace(stderrTail.String())
		if detail == "" {
			detail = waitErr.Error()
		}
		return fmt.Errorf("TTS bridge failed: %s", detail)
	}
	return nil
}

func scanBridgeOutput(reader io.Reader, progress ProgressCallback, tail *boundedBuffer) {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 4096), 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		if tail != nil {
			_, _ = tail.Write([]byte(line + "\n"))
		}
		if progress == nil || !strings.HasPrefix(line, "RMP_PROGRESS ") {
			continue
		}
		var event bridgeProgress
		if json.Unmarshal([]byte(strings.TrimPrefix(line, "RMP_PROGRESS ")), &event) == nil {
			progress(clamp(event.Progress, 0, 1), event.Step)
		}
	}
}

func (r bridgeRunner) materializeScript(name string) (string, error) {
	if strings.ContainsAny(name, `/\\`) {
		return "", errors.New("invalid bridge script name")
	}
	contents, err := bridgeFiles.ReadFile("bridge/" + name)
	if err != nil {
		return "", fmt.Errorf("read embedded bridge: %w", err)
	}
	directory := filepath.Join(r.ModelsDir, "bridges")
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return "", err
	}
	destination := filepath.Join(directory, name)
	current, _ := os.ReadFile(destination)
	if bytes.Equal(current, contents) {
		return destination, nil
	}
	temporary, err := os.CreateTemp(directory, ".bridge-*")
	if err != nil {
		return "", err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if _, err := temporary.Write(contents); err != nil {
		_ = temporary.Close()
		return "", err
	}
	if err := temporary.Chmod(0o700); err != nil {
		_ = temporary.Close()
		return "", err
	}
	if err := temporary.Close(); err != nil {
		return "", err
	}
	if runtime.GOOS == "windows" {
		_ = os.Remove(destination)
	}
	if err := os.Rename(temporaryPath, destination); err != nil {
		return "", err
	}
	return destination, nil
}

func resolvePython(configured string) (string, []string, error) {
	if configured = strings.TrimSpace(configured); configured != "" {
		// Treat the configured value as one executable path. Splitting on spaces
		// would break common Windows locations such as Program Files.
		binary, err := exec.LookPath(configured)
		if err != nil {
			return "", nil, fmt.Errorf("configured Python executable not found: %s", configured)
		}
		return binary, nil, nil
	}
	candidates := [][]string{{"python3"}, {"python"}}
	if runtime.GOOS == "windows" {
		candidates = append([][]string{{"py", "-3"}}, candidates...)
	}
	for _, candidate := range candidates {
		if binary, err := exec.LookPath(candidate[0]); err == nil {
			return binary, candidate[1:], nil
		}
	}
	return "", nil, errors.New("Python 3 was not found; install Python and the selected TTS package")
}

type boundedBuffer struct {
	buffer bytes.Buffer
}

const maxTailBytes = 32 * 1024

func (b *boundedBuffer) Write(p []byte) (int, error) {
	if len(p) >= maxTailBytes {
		b.buffer.Reset()
		_, _ = b.buffer.Write(p[len(p)-maxTailBytes:])
		return len(p), nil
	}
	if b.buffer.Len()+len(p) > maxTailBytes {
		current := b.buffer.Bytes()
		drop := b.buffer.Len() + len(p) - maxTailBytes
		remaining := append([]byte(nil), current[drop:]...)
		b.buffer.Reset()
		_, _ = b.buffer.Write(remaining)
	}
	_, _ = b.buffer.Write(p)
	return len(p), nil
}

func (b *boundedBuffer) String() string { return b.buffer.String() }

func prepareChunks(text string, options domain.ProcessingOptions, maxChars int) []string {
	spoken := cleaner.Verbalize(text)
	instance := cleaner.NewScientificTextCleaner(options)
	return instance.SplitText(spoken, maxChars)
}

func validateAudio(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("audio was not produced: %w", err)
	}
	if info.Size() < 44 {
		return errors.New("generated WAV is empty")
	}
	duration, err := util.WAVDuration(path)
	if err != nil {
		return fmt.Errorf("generated audio is invalid: %w", err)
	}
	if duration <= 0 {
		return errors.New("generated WAV has zero duration")
	}
	return nil
}

func clamp(value, minimum, maximum float64) float64 {
	if value < minimum {
		return minimum
	}
	if value > maximum {
		return maximum
	}
	return value
}
