package config

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

const AppName = "ReadMyPaper"
const pythonEnvName = "READMYPAPER_PYTHON_BIN"

// Settings centralizes operational limits and filesystem locations. Values can
// be overridden with the same READMYPAPER_* environment variables as the
// Python application where practical.
type Settings struct {
	DataDir           string
	CacheDir          string
	MaxWorkers        int
	MaxUploadBytes    int64
	MaxPDFPages       int
	SpeechRateMin     float64
	SpeechRateMax     float64
	MaxPendingJobs    int
	JobRetentionHours int
	LLMBaseURL        string
	LLMModel          string
	LLMEnabled        bool
	LLMAPIKey         string
	PythonBinary      string
}

func Load() (Settings, error) {
	dataDir, err := defaultDataDir()
	if err != nil {
		return Settings{}, err
	}
	cacheDir, err := defaultCacheDir()
	if err != nil {
		return Settings{}, err
	}

	s := Settings{
		DataDir:           envPath("READMYPAPER_DATA_DIR", dataDir),
		CacheDir:          envPath("READMYPAPER_CACHE_DIR", cacheDir),
		MaxWorkers:        envInt("READMYPAPER_MAX_WORKERS", 2),
		MaxUploadBytes:    envInt64("READMYPAPER_MAX_UPLOAD_BYTES", 50*1024*1024),
		MaxPDFPages:       envInt("READMYPAPER_MAX_PDF_PAGES", 200),
		SpeechRateMin:     envFloat("READMYPAPER_SPEECH_RATE_MIN", 0.5),
		SpeechRateMax:     envFloat("READMYPAPER_SPEECH_RATE_MAX", 2.0),
		MaxPendingJobs:    envInt("READMYPAPER_MAX_PENDING_JOBS", 10),
		JobRetentionHours: envInt("READMYPAPER_JOB_RETENTION_HOURS", 0),
		LLMBaseURL:        strings.TrimSpace(os.Getenv("READMYPAPER_LLM_URL")),
		LLMModel:          strings.TrimSpace(os.Getenv("READMYPAPER_LLM_MODEL")),
		LLMEnabled:        envBool("READMYPAPER_LLM_ENABLED", false),
		LLMAPIKey:         envString("READMYPAPER_LLM_API_KEY", "apikey"),
		PythonBinary:      ResolvePythonBinary(),
	}
	if s.MaxWorkers < 1 {
		s.MaxWorkers = 1
	}
	if s.MaxUploadBytes < 1 {
		return Settings{}, fmt.Errorf("READMYPAPER_MAX_UPLOAD_BYTES must be positive")
	}
	if s.MaxPendingJobs < 1 {
		s.MaxPendingJobs = 1
	}
	if s.MaxPDFPages < 1 {
		return Settings{}, fmt.Errorf("READMYPAPER_MAX_PDF_PAGES must be positive")
	}
	if s.SpeechRateMin <= 0 || s.SpeechRateMax < s.SpeechRateMin {
		return Settings{}, fmt.Errorf("invalid speech-rate limits")
	}
	if s.JobRetentionHours < 0 {
		s.JobRetentionHours = 0
	}
	if err := s.EnsureDirs(); err != nil {
		return Settings{}, err
	}
	return s, nil
}

func (s Settings) UploadsDir() string { return filepath.Join(s.DataDir, "uploads") }
func (s Settings) OutputsDir() string { return filepath.Join(s.DataDir, "outputs") }
func (s Settings) VoicesDir() string  { return filepath.Join(s.CacheDir, "voices") }
func (s Settings) ModelsDir() string  { return filepath.Join(s.CacheDir, "models") }

func ResolvePythonBinary() string {
	executable, err := os.Executable()
	if err != nil {
		return strings.TrimSpace(os.Getenv(pythonEnvName))
	}
	return resolvePythonBinaryForExecutable(executable, os.Getenv)
}

func resolvePythonBinaryForExecutable(executable string, getenv func(string) string) string {
	if configured := strings.TrimSpace(getenv(pythonEnvName)); configured != "" {
		return configured
	}
	if bundled, ok := bundledPythonForExecutable(executable); ok {
		return bundled
	}
	return ""
}

func bundledPythonForExecutable(executable string) (string, bool) {
	executable = filepath.Clean(executable)
	macOSDir := filepath.Dir(executable)
	if filepath.Base(macOSDir) != "MacOS" {
		return "", false
	}
	contentsDir := filepath.Dir(macOSDir)
	if filepath.Base(contentsDir) != "Contents" {
		return "", false
	}
	appDir := filepath.Dir(contentsDir)
	if !strings.HasSuffix(filepath.Base(appDir), ".app") {
		return "", false
	}
	for _, resourceDir := range bundledPythonResourceDirs() {
		python := filepath.Join(contentsDir, "Resources", resourceDir, "bin", "python3")
		info, err := os.Stat(python)
		if err == nil && !info.IsDir() {
			return python, true
		}
	}
	return "", false
}

func bundledPythonResourceDirs() []string {
	arch := runtime.GOARCH
	if arch == "amd64" {
		arch = "x86_64"
	}
	if arch == "" {
		return []string{"python"}
	}
	return []string{"python", "python-" + arch}
}

func (s Settings) EnsureDirs() error {
	for _, dir := range []string{
		s.DataDir,
		s.CacheDir,
		s.UploadsDir(),
		s.OutputsDir(),
		s.VoicesDir(),
		s.ModelsDir(),
	} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("create %s: %w", dir, err)
		}
	}
	return nil
}

func defaultDataDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}
	switch runtime.GOOS {
	case "darwin":
		return filepath.Join(home, "Library", "Application Support", AppName), nil
	case "windows":
		if local := os.Getenv("LOCALAPPDATA"); local != "" {
			return filepath.Join(local, AppName), nil
		}
		return filepath.Join(home, "AppData", "Local", AppName), nil
	default:
		if xdg := os.Getenv("XDG_DATA_HOME"); xdg != "" {
			return filepath.Join(xdg, AppName), nil
		}
		return filepath.Join(home, ".local", "share", AppName), nil
	}
}

func defaultCacheDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}
	switch runtime.GOOS {
	case "darwin":
		return filepath.Join(home, "Library", "Caches", AppName), nil
	case "windows":
		if local := os.Getenv("LOCALAPPDATA"); local != "" {
			return filepath.Join(local, AppName, "Cache"), nil
		}
		return filepath.Join(home, "AppData", "Local", AppName, "Cache"), nil
	default:
		if xdg := os.Getenv("XDG_CACHE_HOME"); xdg != "" {
			return filepath.Join(xdg, AppName), nil
		}
		return filepath.Join(home, ".cache", AppName), nil
	}
}

func envPath(name, fallback string) string {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		value = fallback
	}
	if strings.HasPrefix(value, "~"+string(filepath.Separator)) {
		if home, err := os.UserHomeDir(); err == nil {
			value = filepath.Join(home, strings.TrimPrefix(value, "~"+string(filepath.Separator)))
		}
	}
	if abs, err := filepath.Abs(value); err == nil {
		return abs
	}
	return filepath.Clean(value)
}

func envInt(name string, fallback int) int {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func envInt64(name string, fallback int64) int64 {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return fallback
	}
	return parsed
}

func envFloat(name string, fallback float64) float64 {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return fallback
	}
	return parsed
}

func envBool(name string, fallback bool) bool {
	value := strings.TrimSpace(strings.ToLower(os.Getenv(name)))
	if value == "" {
		return fallback
	}
	return value == "1" || value == "true" || value == "yes" || value == "on"
}

func envString(name, fallback string) string {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}
	return value
}
