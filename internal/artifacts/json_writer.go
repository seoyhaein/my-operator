package artifacts

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/yeongki/my-operator/pkg/slo"
)

type JSONFileWriter struct {
	Path string
}

func (w JSONFileWriter) WriteSummary(s slo.Summary) error {
	if w.Path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(w.Path), 0o755); err != nil {
		return err
	}

	tmp := w.Path + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}

	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")

	if err := enc.Encode(s); err != nil {
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
