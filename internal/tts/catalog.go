package tts

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const piperVoiceBaseURL = "https://huggingface.co/rhasspy/piper-voices/resolve/main"

// VoiceSpec describes a selectable TTS voice. Piper fields are local model
// artifacts; KokoroVoice is the identifier consumed by kokoro-python.
type VoiceSpec struct {
	Key            string `json:"key"`
	LanguageCode   string `json:"language_code"`
	LanguageLabel  string `json:"language_label"`
	DisplayName    string `json:"display_name"`
	Engine         string `json:"engine"`
	Folder         string `json:"folder,omitempty"`
	ModelFilename  string `json:"model_filename,omitempty"`
	ConfigFilename string `json:"config_filename,omitempty"`
	KokoroVoice    string `json:"kokoro_voice,omitempty"`
}

func (v VoiceSpec) ModelURL() string {
	return piperVoiceBaseURL + "/" + strings.Trim(v.Folder, "/") + "/" + v.ModelFilename
}

func (v VoiceSpec) ConfigURL() string {
	return piperVoiceBaseURL + "/" + strings.Trim(v.Folder, "/") + "/" + v.ConfigFilename
}

var voiceSpecs = map[string]VoiceSpec{
	"en_US-lessac-medium": {
		Key: "en_US-lessac-medium", LanguageCode: "en", LanguageLabel: "English",
		DisplayName: "English — Lessac (Piper, fast)", Engine: "piper",
		Folder: "en/en_US/lessac/medium", ModelFilename: "en_US-lessac-medium.onnx", ConfigFilename: "en_US-lessac-medium.onnx.json",
	},
	"en_US-hfc_female-medium": {
		Key: "en_US-hfc_female-medium", LanguageCode: "en", LanguageLabel: "English",
		DisplayName: "English — HFC Female (Piper, fast)", Engine: "piper",
		Folder: "en/en_US/hfc_female/medium", ModelFilename: "en_US-hfc_female-medium.onnx", ConfigFilename: "en_US-hfc_female-medium.onnx.json",
	},
	"pt_BR-faber-medium": {
		Key: "pt_BR-faber-medium", LanguageCode: "pt-BR", LanguageLabel: "Brazilian Portuguese",
		DisplayName: "Brazilian Portuguese — Faber (Piper, fast)", Engine: "piper",
		Folder: "pt/pt_BR/faber/medium", ModelFilename: "pt_BR-faber-medium.onnx", ConfigFilename: "pt_BR-faber-medium.onnx.json",
	},
	"pt_BR-cadu-medium": {
		Key: "pt_BR-cadu-medium", LanguageCode: "pt-BR", LanguageLabel: "Brazilian Portuguese",
		DisplayName: "Brazilian Portuguese — Cadu (Piper, fast)", Engine: "piper",
		Folder: "pt/pt_BR/cadu/medium", ModelFilename: "pt_BR-cadu-medium.onnx", ConfigFilename: "pt_BR-cadu-medium.onnx.json",
	},
	"kokoro-en-heart": {
		Key: "kokoro-en-heart", LanguageCode: "en", LanguageLabel: "English",
		DisplayName: "English — Heart (Kokoro, quality)", Engine: "kokoro", KokoroVoice: "af_heart",
	},
	"kokoro-en-michael": {
		Key: "kokoro-en-michael", LanguageCode: "en", LanguageLabel: "English",
		DisplayName: "English — Michael (Kokoro, quality)", Engine: "kokoro", KokoroVoice: "am_michael",
	},
	"kokoro-en-bella": {
		Key: "kokoro-en-bella", LanguageCode: "en", LanguageLabel: "English",
		DisplayName: "English — Bella (Kokoro, quality)", Engine: "kokoro", KokoroVoice: "af_bella",
	},
	"kokoro-pt-dora": {
		Key: "kokoro-pt-dora", LanguageCode: "pt-BR", LanguageLabel: "Brazilian Portuguese",
		DisplayName: "Brazilian Portuguese — Dora (Kokoro, quality)", Engine: "kokoro", KokoroVoice: "pf_dora",
	},
}

var defaultPiper = map[string]string{
	"en": "en_US-lessac-medium", "en-us": "en_US-lessac-medium",
	"pt": "pt_BR-faber-medium", "pt-br": "pt_BR-faber-medium",
}

var defaultKokoro = map[string]string{
	"en": "kokoro-en-heart", "en-us": "kokoro-en-heart",
	"pt": "kokoro-pt-dora", "pt-br": "kokoro-pt-dora",
}

type Catalog struct {
	Root       string
	HTTPClient *http.Client
	downloadMu sync.Mutex
}

func NewCatalog(root string) *Catalog {
	return &Catalog{Root: root, HTTPClient: &http.Client{Timeout: 20 * time.Minute}}
}

func (c *Catalog) List() []VoiceSpec {
	result := make([]VoiceSpec, 0, len(voiceSpecs))
	for _, spec := range voiceSpecs {
		result = append(result, spec)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Engine != result[j].Engine {
			return result[i].Engine < result[j].Engine
		}
		return result[i].DisplayName < result[j].DisplayName
	})
	return result
}

func (c *Catalog) Resolve(requested, language, engine string) VoiceSpec {
	requested = strings.TrimSpace(requested)
	engine = normalizeEngine(engine)
	if requested != "" && requested != "auto" {
		if spec, exists := voiceSpecs[requested]; exists && spec.Engine == engine {
			return spec
		}
	}
	language = strings.ToLower(strings.TrimSpace(language))
	defaults := defaultPiper
	if engine == "kokoro" {
		defaults = defaultKokoro
	}
	if key, exists := defaults[language]; exists {
		return voiceSpecs[key]
	}
	if strings.HasPrefix(language, "pt") {
		return voiceSpecs[defaults["pt-br"]]
	}
	return voiceSpecs[defaults["en"]]
}

func (c *Catalog) IsCompatible(voiceKey, engine string) bool {
	if voiceKey == "" || voiceKey == "auto" {
		return true
	}
	spec, exists := voiceSpecs[voiceKey]
	return !exists || spec.Engine == normalizeEngine(engine)
}

func normalizeEngine(value string) string {
	if strings.EqualFold(strings.TrimSpace(value), "kokoro") {
		return "kokoro"
	}
	return "piper"
}

// EnsureDownloaded atomically fetches the two Piper voice files. Kokoro owns
// its model cache and therefore returns empty paths.
func (c *Catalog) EnsureDownloaded(ctx context.Context, spec VoiceSpec, progress ProgressCallback) (string, string, error) {
	if spec.Engine == "kokoro" {
		return "", "", nil
	}
	if c == nil || strings.TrimSpace(c.Root) == "" {
		return "", "", fmt.Errorf("voice cache directory is not configured")
	}
	c.downloadMu.Lock()
	defer c.downloadMu.Unlock()
	if c.HTTPClient == nil {
		c.HTTPClient = &http.Client{Timeout: 20 * time.Minute}
	}
	voiceDir := filepath.Join(c.Root, spec.Key)
	if err := os.MkdirAll(voiceDir, 0o755); err != nil {
		return "", "", fmt.Errorf("create voice directory: %w", err)
	}
	modelPath := filepath.Join(voiceDir, spec.ModelFilename)
	configPath := filepath.Join(voiceDir, spec.ConfigFilename)
	if progress != nil {
		progress(0, "Preparing voice model")
	}
	if err := c.downloadIfMissing(ctx, spec.ModelURL(), modelPath, func(done, total int64) {
		if progress == nil || total <= 0 {
			return
		}
		progress(0.72*float64(done)/float64(total), "Downloading Piper voice")
	}); err != nil {
		return "", "", fmt.Errorf("download Piper model: %w", err)
	}
	if err := c.downloadIfMissing(ctx, spec.ConfigURL(), configPath, nil); err != nil {
		return "", "", fmt.Errorf("download Piper configuration: %w", err)
	}
	if progress != nil {
		progress(1, "Voice model ready")
	}
	return modelPath, configPath, nil
}

func (c *Catalog) downloadIfMissing(ctx context.Context, sourceURL, destination string, progress func(int64, int64)) error {
	if info, err := os.Stat(destination); err == nil && info.Size() > 0 {
		return nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, sourceURL, nil)
	if err != nil {
		return err
	}
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("HTTP %s", resp.Status)
	}
	temporary, err := os.CreateTemp(filepath.Dir(destination), ".download-*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	committed := false
	defer func() {
		_ = temporary.Close()
		if !committed {
			_ = os.Remove(temporaryPath)
		}
	}()
	reader := &progressReader{Reader: resp.Body, Total: resp.ContentLength, Report: progress}
	if _, err := io.Copy(temporary, reader); err != nil {
		return err
	}
	if err := temporary.Sync(); err != nil {
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if info, err := os.Stat(temporaryPath); err != nil || info.Size() == 0 {
		return fmt.Errorf("downloaded file is empty")
	}
	// A previous interrupted download may have left an empty destination.
	// Removing it also makes the atomic rename work on Windows, where rename
	// does not replace an existing file.
	if info, err := os.Stat(destination); err == nil && info.Size() == 0 {
		if err := os.Remove(destination); err != nil {
			return fmt.Errorf("remove stale voice file: %w", err)
		}
	}
	if err := os.Rename(temporaryPath, destination); err != nil {
		return err
	}
	committed = true
	return nil
}

type progressReader struct {
	io.Reader
	Done   int64
	Total  int64
	Report func(int64, int64)
}

func (r *progressReader) Read(p []byte) (int, error) {
	n, err := r.Reader.Read(p)
	r.Done += int64(n)
	if r.Report != nil {
		r.Report(r.Done, r.Total)
	}
	return n, err
}
