package app

import (
	"errors"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"sync"
)

// AudioPlayer delegates WAV playback to a small platform-native command. This
// keeps the app binary free from a second audio stack while preserving local
// playback and an explicit stop operation.
type AudioPlayer struct {
	mu  sync.Mutex
	cmd *exec.Cmd
}

func (p *AudioPlayer) Play(path string) error {
	p.Stop()
	command, err := playbackCommand(path)
	if err != nil {
		return err
	}
	if err := command.Start(); err != nil {
		return err
	}
	p.mu.Lock()
	p.cmd = command
	p.mu.Unlock()
	go func() {
		_ = command.Wait()
		p.mu.Lock()
		if p.cmd == command {
			p.cmd = nil
		}
		p.mu.Unlock()
	}()
	return nil
}

func (p *AudioPlayer) Stop() {
	p.mu.Lock()
	command := p.cmd
	p.cmd = nil
	p.mu.Unlock()
	if command != nil && command.Process != nil {
		_ = command.Process.Kill()
	}
}

func playbackCommand(path string) (*exec.Cmd, error) {
	switch runtime.GOOS {
	case "darwin":
		if binary, err := exec.LookPath("afplay"); err == nil {
			return exec.Command(binary, path), nil
		}
	case "windows":
		binary, err := exec.LookPath("powershell.exe")
		if err != nil {
			binary, err = exec.LookPath("powershell")
		}
		if err == nil {
			escaped := strings.ReplaceAll(path, "'", "''")
			script := fmt.Sprintf("$p=New-Object System.Media.SoundPlayer '%s'; $p.PlaySync()", escaped)
			return exec.Command(binary, "-NoProfile", "-NonInteractive", "-Command", script), nil
		}
	default:
		candidates := [][]string{
			{"ffplay", "-nodisp", "-autoexit", "-loglevel", "quiet", path},
			{"paplay", path},
			{"aplay", path},
		}
		for _, candidate := range candidates {
			if binary, err := exec.LookPath(candidate[0]); err == nil {
				return exec.Command(binary, candidate[1:]...), nil
			}
		}
	}
	return nil, errors.New("no supported local WAV player was found (afplay, ffplay, paplay, aplay, or PowerShell)")
}
