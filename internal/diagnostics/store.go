package diagnostics

import (
	"errors"
	"os"
	"path/filepath"
)

const (
	eventsFile  = "events.jsonl"
	rotatedFile = "events.jsonl.1"
	reportsDir  = "reports"
)

var stateDirFn = defaultStateDir

func appendEvent(event Event) error {
	appendMu.Lock()
	defer appendMu.Unlock()

	dir, err := stateDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	path := filepath.Join(dir, eventsFile)
	if err := rotateIfNeeded(path); err != nil {
		return err
	}
	return appendLine(path, event)
}

func appendLine(path string, event Event) error {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return err
	}
	defer file.Close()

	data, err := Marshal(event)
	if err != nil {
		return err
	}
	data = append(data, '\n')
	_, err = file.Write(data)
	return err
}

func rotateIfNeeded(path string) error {
	info, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil || info.Size() <= maxBytes {
		return err
	}
	rotated := filepath.Join(filepath.Dir(path), rotatedFile)
	_ = os.Remove(rotated)
	return os.Rename(path, rotated)
}

func stateDir() (string, error) {
	if dir := os.Getenv("PRE_DIAGNOSTICS_DIR"); dir != "" {
		return dir, nil
	}
	return stateDirFn()
}

func defaultStateDir() (string, error) {
	if dir := os.Getenv("XDG_STATE_HOME"); dir != "" {
		return filepath.Join(dir, "pre"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "state", "pre"), nil
}

func eventPaths(dir string) []string {
	return []string{
		filepath.Join(dir, rotatedFile),
		filepath.Join(dir, eventsFile),
	}
}
