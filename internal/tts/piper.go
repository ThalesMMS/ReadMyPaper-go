package tts

import (
	"context"
	"fmt"
	"math"
	"os"
	"path/filepath"

	"github.com/ThalesMMS/ReadMyPaper-go/internal/domain"
)

type PiperEngine struct {
	Catalog      *Catalog
	PythonBinary string
	ModelsDir    string
}

func (PiperEngine) Name() string { return "piper" }

type piperRequest struct {
	OutputPath string   `json:"output_path"`
	ModelPath  string   `json:"model_path"`
	ConfigPath string   `json:"config_path"`
	Chunks     []string `json:"chunks"`
	SpeechRate float64  `json:"speech_rate"`
	PauseMS    int      `json:"pause_ms"`
}

func (e PiperEngine) Synthesize(
	ctx context.Context,
	text, outputPath string,
	options domain.ProcessingOptions,
	voice VoiceSpec,
	progress ProgressCallback,
) (VoiceSpec, error) {
	if voice.Engine != "piper" {
		return voice, fmt.Errorf("voice %q is not a Piper voice", voice.Key)
	}
	if e.Catalog == nil {
		return voice, fmt.Errorf("Piper voice catalog is not configured")
	}
	if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil {
		return voice, err
	}
	modelPath, configPath, err := e.Catalog.EnsureDownloaded(ctx, voice, func(ratio float64, step string) {
		if progress != nil {
			progress(ratio*0.18, step)
		}
	})
	if err != nil {
		return voice, err
	}
	chunks := prepareChunks(text, options, options.ChunkMaxChars)
	if len(chunks) == 0 {
		return voice, fmt.Errorf("there is no cleaned text to synthesize")
	}
	request := piperRequest{
		OutputPath: outputPath, ModelPath: modelPath, ConfigPath: configPath,
		Chunks: chunks, SpeechRate: clamp(options.SpeechRate, 0.7, 1.4), PauseMS: options.PauseMS,
	}
	runner := bridgeRunner{PythonBinary: e.PythonBinary, ModelsDir: e.ModelsDir}
	if err := runner.run(ctx, "piper_bridge.py", request, func(ratio float64, step string) {
		if progress != nil {
			progress(0.18+ratio*0.82, step)
		}
	}); err != nil {
		return voice, fmt.Errorf("Piper synthesis unavailable (install with `python -m pip install piper-tts`): %w", err)
	}
	if err := validateAudio(outputPath); err != nil {
		return voice, err
	}
	if progress != nil {
		progress(1, fmt.Sprintf("Audio ready (%d chunks)", len(chunks)))
	}
	return voice, nil
}

// PiperLengthScale is exported for tests and mirrors Piper's inverse-rate
// control. The Python bridge performs this conversion before synthesis.
func PiperLengthScale(speechRate float64) float64 {
	safe := clamp(speechRate, 0.7, 1.4)
	return math.Round((1/safe)*1000) / 1000
}
