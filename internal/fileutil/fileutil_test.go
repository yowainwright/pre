package fileutil

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

type mockFile struct {
	name     string
	writeErr error
	chmodErr error
	syncErr  error
	closeErr error
}

func (m *mockFile) Write([]byte) (int, error) { return 0, m.writeErr }
func (m *mockFile) Chmod(os.FileMode) error   { return m.chmodErr }
func (m *mockFile) Sync() error               { return m.syncErr }
func (m *mockFile) Close() error              { return m.closeErr }
func (m *mockFile) Name() string              { return m.name }

func withCreateTemp(fn func(string, string) (writableFile, error)) func() {
	orig := createTempFn
	createTempFn = fn
	return func() { createTempFn = orig }
}

func mockCreateTemp(f *mockFile) func() {
	return withCreateTemp(func(_, _ string) (writableFile, error) { return f, nil })
}

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

func TestAtomicWriteFileWriteError(t *testing.T) {
	dir := t.TempDir()
	f := &mockFile{name: filepath.Join(dir, "tmp"), writeErr: errors.New("write fail")}
	defer mockCreateTemp(f)()

	err := AtomicWriteFile(filepath.Join(dir, "out.json"), []byte("data"), 0644)
	if err == nil || err.Error() != "write fail" {
		t.Errorf("expected write error, got %v", err)
	}
}

func TestAtomicWriteFileChmodError(t *testing.T) {
	dir := t.TempDir()
	f := &mockFile{name: filepath.Join(dir, "tmp"), chmodErr: errors.New("chmod fail")}
	defer mockCreateTemp(f)()

	err := AtomicWriteFile(filepath.Join(dir, "out.json"), []byte("data"), 0644)
	if err == nil || err.Error() != "chmod fail" {
		t.Errorf("expected chmod error, got %v", err)
	}
}

func TestAtomicWriteFileSyncError(t *testing.T) {
	dir := t.TempDir()
	f := &mockFile{name: filepath.Join(dir, "tmp"), syncErr: errors.New("sync fail")}
	defer mockCreateTemp(f)()

	err := AtomicWriteFile(filepath.Join(dir, "out.json"), []byte("data"), 0644)
	if err == nil || err.Error() != "sync fail" {
		t.Errorf("expected sync error, got %v", err)
	}
}

func TestAtomicWriteFileCloseError(t *testing.T) {
	dir := t.TempDir()
	f := &mockFile{name: filepath.Join(dir, "tmp"), closeErr: errors.New("close fail")}
	defer mockCreateTemp(f)()

	err := AtomicWriteFile(filepath.Join(dir, "out.json"), []byte("data"), 0644)
	if err == nil || err.Error() != "close fail" {
		t.Errorf("expected close error, got %v", err)
	}
}
