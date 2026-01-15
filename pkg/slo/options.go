package slo

import (
	"path/filepath"
	"time"
)

// Options are injected by the caller (no env dependency in this package).
type Options struct {
	Enabled bool

	// ArtifactsDir is where sli-summary.json will be written.
	// Caller decides the directory; library never reads env.
	ArtifactsDir string

	// RunID is optional metadata (e.g., CI run id).
	RunID string

	// Test hooks / toggles (still injected, not env-driven here)
	SkipCleanup            bool
	SkipCertManagerInstall bool

	// Token / metrics related knobs (for later TODOs)
	TokenRequestTimeout time.Duration
}

// Validate applies safe defaults and returns a normalized copy.
func (o Options) Validate() Options {
	out := o

	if out.ArtifactsDir == "" {
		out.ArtifactsDir = "/tmp"
	}
	if out.TokenRequestTimeout == 0 {
		out.TokenRequestTimeout = 2 * time.Minute
	}
	return out
}

func (o Options) SummaryPath() string {
	v := o.Validate()
	return filepath.Join(v.ArtifactsDir, "sli-summary.json")
}
