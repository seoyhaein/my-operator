package artifacts

import (
	"encoding/json"
	"os"
	"path/filepath"
)

type JSONFileWriter struct {
	Path string
}

/*func (w JSONFileWriter) WriteSummary(s slo.Summary) error {
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
}*/

// WriteJSON persists v as pretty-printed JSON to w.Path using an atomic write pattern.
//
// Why this pattern:
//   - We first write the full content into a temporary file (w.Path + ".tmp").
//   - Only after Encode succeeds and the file is closed, we replace the final file via os.Rename.
//   - This avoids leaving a partially-written/corrupted final JSON if the process crashes or errors mid-write.
//
// Notes:
//   - The temp file is created in the same directory as the target file, so Rename is atomic on most Unix filesystems.
//   - In rare cases (e.g., SIGKILL / power loss) the ".tmp" file may remain, but the final file is kept intact.
//   - If Path is empty, this is a no-op (useful for "writer disabled" mode).
func (w JSONFileWriter) WriteJSON(v any) error {
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

	if err := enc.Encode(v); err != nil {
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
