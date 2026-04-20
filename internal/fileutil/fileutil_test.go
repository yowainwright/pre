package fileutil

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAtomicWriteFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.json")
	data := []byte(`{"key":"value"}`)

	if err := AtomicWriteFile(path, data, 0644); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read failed: %v", err)
	}
	if string(got) != string(data) {
		t.Errorf("expected %q, got %q", data, got)
	}
}

func TestAtomicWriteFileOverwrites(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.json")

	if err := AtomicWriteFile(path, []byte("first"), 0644); err != nil {
		t.Fatalf("first write: %v", err)
	}
	if err := AtomicWriteFile(path, []byte("second"), 0644); err != nil {
		t.Fatalf("second write: %v", err)
	}
	got, _ := os.ReadFile(path)
	if string(got) != "second" {
		t.Errorf("expected %q, got %q", "second", got)
	}
}

func TestAtomicWriteFileCreateTempError(t *testing.T) {
	err := AtomicWriteFile("/nonexistent/dir/file.json", []byte("data"), 0644)
	if err == nil {
		t.Error("expected error for nonexistent directory")
	}
}
