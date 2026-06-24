package tts

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/ThalesMMS/ReadMyPaper-go/internal/domain"
)

type KokoroEngine struct {
	PythonBinary string
	ModelsDir    string
}

func (KokoroEngine) Name() string { return "kokoro" }

type kokoroRequest struct {
	OutputPath string   `json:"output_path"`
	Chunks     []string `json:"chunks"`
	Voice      string   `json:"voice"`
	Language   string   `json:"language"`
	Speed      float64  `json:"speed"`
}

func (e KokoroEngine) Synthesize(
	ctx context.Context,
	text, outputPath string,
	options domain.ProcessingOptions,
	voice VoiceSpec,
	progress ProgressCallback,
) (VoiceSpec, error) {
	if voice.Engine != "kokoro" {
		return voice, fmt.Errorf("voice %q is not a Kokoro voice", voice.Key)
	}
	if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil {
		return voice, err
	}
	limit := options.ChunkMaxChars
	if limit < 2000 {
		limit = 2000
	}
	chunks := prepareChunks(text, options, limit)
	if len(chunks) == 0 {
		return voice, fmt.Errorf("there is no cleaned text to synthesize")
	}
	language := "a"
	if len(voice.LanguageCode) >= 2 && voice.LanguageCode[:2] == "pt" {
		language = "p"
	}
	request := kokoroRequest{
		OutputPath: outputPath, Chunks: chunks, Voice: voice.KokoroVoice,
		Language: language, Speed: clamp(options.SpeechRate, 0.5, 2.0),
	}
	runner := bridgeRunner{PythonBinary: e.PythonBinary, ModelsDir: e.ModelsDir}
	if err := runner.run(ctx, "kokoro_bridge.py", request, progress); err != nil {
		return voice, fmt.Errorf("Kokoro synthesis unavailable (install with `python -m pip install kokoro numpy` and install espeak-ng): %w", err)
	}
	if err := validateAudio(outputPath); err != nil {
		return voice, err
	}
	if progress != nil {
		progress(1, fmt.Sprintf("Audio ready (%d chunks)", len(chunks)))
	}
	return voice, nil
}
