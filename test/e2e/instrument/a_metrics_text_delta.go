// internal/instrument/a_metrics_text_delta.go
package instrument

import (
	"fmt"
	"time"

	"github.com/yeongki/my-operator/pkg/slo"
)

// [OLD] legacy json schema (kept for reference)
// ------------------------------------------------
//
// type Status string
//
// const (
// 	StatusPass Status = "pass"
// 	StatusSkip Status = "skip"
// )
//
// type Summary struct {
// 	RunID string `json:"run_id"`
//
// 	Status     Status `json:"status"`
// 	SkipReason string `json:"skip_reason,omitempty"`
//
// 	MetricsSnapshotStartAt string `json:"metrics_snapshot_start_at,omitempty"`
// 	MetricsSnapshotEndAt   string `json:"metrics_snapshot_end_at,omitempty"`
//
// 	ReconcileTotalStart float64 `json:"reconcile_total_start,omitempty"`
// 	ReconcileTotalEnd   float64 `json:"reconcile_total_end,omitempty"`
// 	ReconcileTotalDelta float64 `json:"reconcile_total_delta,omitempty"`
// }
//
// ------------------------------------------------

// ADeltaInstrument is a text-injected instrument for A-option.
// - It accepts "/metrics text" (usually from curl pod logs) directly.
// - It writes slo.Summary (same schema as the unified Instrument).
// - Best-effort: measurement failure must NOT fail tests.
type ADeltaInstrument struct {
	enabled bool

	logf   func(string, ...any)
	writer slo.SummaryWriter
	labels slo.Labels

	metricName string

	runID    string
	startAt  time.Time
	startV   int64
	hasStart bool
}

// NewADelta creates a text-injected instrument.
//
// NOTE:
//   - This is still useful when the caller already has metrics text and wants to
//     reuse the same summary-writing rules as Instrument.
//   - For new code, prefer Instrument + fetcher injection.
func NewADelta(labels slo.Labels, logger slo.Logger, writer slo.SummaryWriter) *ADeltaInstrument {
	i := &ADeltaInstrument{
		enabled:    false, // default off
		logf:       func(string, ...any) {},
		writer:     writer,
		labels:     labels,
		metricName: "controller_runtime_reconcile_total",
	}
	if logger != nil {
		i.logf = logger.Logf
	}
	return i
}

// StartFromMetricsText records start snapshot from given metrics text.
// Returns skipReason ("" means OK).
func (i *ADeltaInstrument) StartFromMetricsText(text string) (skipReason string) {
	i.runID = time.Now().UTC().Format(time.RFC3339Nano)

	if !i.enabled {
		return "instrumentation disabled"
	}
	i.startAt = time.Now()

	v, err := parseCounterSum(text, i.metricName)
	if err != nil {
		i.logf("[slo-lab] ADelta Start: parse failed: %v", err)
		return fmt.Sprintf("start parse failed: %v", err)
	}

	i.startV = v
	i.hasStart = true
	i.logf("[slo-lab] ADelta Start: %s=%d", i.metricName, v)
	return ""
}

// EndFromMetricsText finalizes with end snapshot and writes summary.
// testPassed: result of actual test assertions.
func (i *ADeltaInstrument) EndFromMetricsText(text string, startSkipReason string, testPassed bool) {
	if !i.enabled {
		return
	}

	endAt := time.Now()

	// Convergence time (best-effort)
	var convSeconds *float64
	if !i.startAt.IsZero() {
		d := endAt.Sub(i.startAt).Seconds()
		convSeconds = &d
	}

	// Determine delta (best-effort)
	var deltaVal *float64

	// If Start decided to skip, keep measurement skip.
	if startSkipReason != "" {
		i.writeBestEffort(endAt, testPassed, convSeconds, nil, startSkipReason)
		return
	}
	if !i.hasStart {
		i.writeBestEffort(endAt, testPassed, convSeconds, nil, "start snapshot missing")
		return
	}

	endV, err := parseCounterSum(text, i.metricName)
	if err != nil {
		i.writeBestEffort(endAt, testPassed, convSeconds, nil, fmt.Sprintf("end parse failed: %v", err))
		return
	}

	d := float64(endV - i.startV)
	if d < 0 {
		i.writeBestEffort(endAt, testPassed, convSeconds, nil, fmt.Sprintf("negative delta: %.0f", d))
		return
	}
	deltaVal = &d

	i.writeBestEffort(endAt, testPassed, convSeconds, deltaVal, "")
}

func (i *ADeltaInstrument) writeBestEffort(now time.Time, testPassed bool, convSeconds *float64, deltaVal *float64, skipReason string) {
	if i.writer == nil {
		i.logf("[slo-lab] ADelta End: writer is nil (skip writing summary)")
		return
	}

	// Determine Result (Rule B)
	finalResult := "success"
	if !testPassed {
		finalResult = "fail"
	} else if convSeconds == nil || deltaVal == nil || skipReason != "" {
		finalResult = "skip"
	}
	i.labels.Result = finalResult

	// If caller wants to persist skip reason, you can put it into Labels (if your slo.Labels has a field),
	// but since we don't know your exact Labels struct, we just log it.
	if skipReason != "" {
		i.logf("[slo-lab] ADelta skip reason: %s", skipReason)
	}

	summary := slo.Summary{
		Labels:    i.labels,
		CreatedAt: now.UTC(),
		Metrics: slo.SummaryMetrics{
			E2EConvergenceTimeSeconds: convSeconds,
			ReconcileTotalDelta:       deltaVal,
		},
	}

	if err := i.writer.WriteSummary(summary); err != nil {
		i.logf("[slo-lab] ADelta End: write summary failed (ignored): %v", err)
		return
	}

	i.logf("[slo-lab] ADelta wrote summary: result=%s conv=%s delta=%s",
		finalResult, formatPtrFloat(convSeconds), formatPtrFloat(deltaVal))
}
