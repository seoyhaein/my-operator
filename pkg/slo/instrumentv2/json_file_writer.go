package instrumentv2

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// JSONFileWriter writes SessionResult into a JSON file (atomic-ish: temp + rename).
type JSONFileWriter struct {
	Path string
}

func NewJSONFileWriter(path string) *JSONFileWriter {
	return &JSONFileWriter{Path: path}
}

func (w *JSONFileWriter) Save(ctx context.Context, session SessionResult) error {
	_ = ctx
	if w == nil || w.Path == "" {
		return nil
	}

	if err := os.MkdirAll(filepath.Dir(w.Path), 0o755); err != nil {
		return err
	}

	tmp := fmt.Sprintf("%s.tmp.%d", w.Path, time.Now().UnixNano())
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}

	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	if err := enc.Encode(session); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, w.Path)
}
