package runtime

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type MarkerStore interface {
	Load() (*int64, error)
	Save(marker int64) error
}

type FileMarkerStore struct {
	path string
}

func NewFileMarkerStore(path string) *FileMarkerStore {
	return &FileMarkerStore{path: strings.TrimSpace(path)}
}

func (s *FileMarkerStore) Load() (*int64, error) {
	if s == nil || s.path == "" {
		return nil, nil
	}

	raw, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read marker file: %w", err)
	}

	value := strings.TrimSpace(string(raw))
	if value == "" {
		return nil, nil
	}

	marker, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("parse marker file: %w", err)
	}

	return &marker, nil
}

func (s *FileMarkerStore) Save(marker int64) error {
	if s == nil || s.path == "" {
		return nil
	}

	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create marker dir: %w", err)
	}

	tmpFile, err := os.CreateTemp(dir, "poll-marker-*")
	if err != nil {
		return fmt.Errorf("create marker temp file: %w", err)
	}

	tmpPath := tmpFile.Name()
	defer func() {
		_ = os.Remove(tmpPath)
	}()

	if _, err := tmpFile.WriteString(strconv.FormatInt(marker, 10)); err != nil {
		_ = tmpFile.Close()
		return fmt.Errorf("write marker temp file: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		return fmt.Errorf("close marker temp file: %w", err)
	}

	if err := os.Rename(tmpPath, s.path); err != nil {
		return fmt.Errorf("replace marker file: %w", err)
	}

	return nil
}
