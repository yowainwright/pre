package fileutil

import (
	"errors"
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

	if err := prepareTempFile(tmp, data, perm); err != nil {
		return closeWithError(tmp, err)
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}

func prepareTempFile(file writableFile, data []byte, perm os.FileMode) error {
	if _, err := file.Write(data); err != nil {
		return err
	}
	if err := file.Chmod(perm); err != nil {
		return err
	}
	return file.Sync()
}

func closeWithError(file writableFile, err error) error {
	if closeErr := file.Close(); closeErr != nil {
		return errors.Join(err, closeErr)
	}
	return err
}
