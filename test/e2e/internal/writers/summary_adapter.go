package writers

import (
	"github.com/yeongki/my-operator/pkg/slo"
	"github.com/yeongki/my-operator/pkg/slo/artifacts"
)

// SummaryWriterAdapter adapts artifacts.JSONFileWriter (generic JSON writer)
// to slo.SummaryWriter (domain-specific writer).
type SummaryWriterAdapter struct {
	W artifacts.JSONFileWriter
}

func (a SummaryWriterAdapter) WriteSummary(s slo.Summary) error {
	return a.W.WriteJSON(s)
}
