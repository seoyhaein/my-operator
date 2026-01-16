package instrumentv2

import (
	"path/filepath"
	"time"
)

type ArtifactOptions struct {
	ArtifactsDir        string
	RunID               string
	TokenRequestTimeout time.Duration
	DefaultArtifactsDir string
	DefaultTokenTimeout time.Duration
}

// Normalize fills safe defaults (caller can override defaults by setting Default* fields).
func (o ArtifactOptions) Normalize() ArtifactOptions {
	out := o
	if out.DefaultArtifactsDir == "" {
		out.DefaultArtifactsDir = "/tmp"
	}
	if out.DefaultTokenTimeout == 0 {
		out.DefaultTokenTimeout = 2 * time.Minute
	}

	if out.ArtifactsDir == "" {
		out.ArtifactsDir = out.DefaultArtifactsDir
	}
	if out.TokenRequestTimeout == 0 {
		out.TokenRequestTimeout = out.DefaultTokenTimeout
	}
	return out
}

// SummaryPath returns a stable artifact path.
// NOTE: If you want per-test file naming, do that in harness/method layer.
func (o ArtifactOptions) SummaryPath(filename string) string {
	v := o.Normalize()
	if filename == "" {
		filename = "sli-summary.json"
	}
	return filepath.Join(v.ArtifactsDir, filename)
}
