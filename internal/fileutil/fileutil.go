package fileutil

import (
	"os"
	"path/filepath"
)

type writableFile interface {
	Write([]byte) (int, error)
	Chmod(os.FileMode) error
	Sync() error
	Close() error
	Name() string
}

var createTempFn = func(dir, pattern string) (writableFile, error) {
	return os.CreateTemp(dir, pattern)
}

func AtomicWriteFile(path string, data []byte, perm os.FileMode) error {
	tmp, err := createTempFn(filepath.Dir(path), filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)

	if _, err := tmp.Write(data); err != nil {
		return closeWithError(tmp, err)
	}
	if err := tmp.Chmod(perm); err != nil {
		return closeWithError(tmp, err)
	}
	if err := tmp.Sync(); err != nil {
		return closeWithError(tmp, err)
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}

func closeWithError(file writableFile, err error) error {
	if closeErr := file.Close(); closeErr != nil {
		return closeErr
	}
	return err
}
