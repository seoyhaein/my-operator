package instrument

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/prometheus/common/expfmt"

	"github.com/yeongki/my-operator/pkg/slo"
)

// MetricsFetcher defines how to get raw Prometheus metrics text.
// test/e2e will provide an implementation (kubectl run curl pod -> logs).
type MetricsFetcher func(ctx context.Context) (string, error)

// Instrument is a unified E2E instrumentation helper.
//
// Rules:
// - Best-effort: measurement failure MUST NOT fail tests.
// - "testPassed" is the source of truth for success/fail.
// - If testPassed=true but measurement missing => result=skip.
type Instrument struct {
	enabled bool
	fetcher MetricsFetcher
	logf    func(string, ...any)
	writer  slo.SummaryWriter

	labels slo.Labels

	startTime time.Time
	hasStart  bool

	// "A-option" like counter snapshot
	reconcileStart int64
	hasRecStart    bool

	// TODO(next): Enhance metric support beyond a single counter.
	//
	// Current limitation:
	//   Only supports 'reconcileTotalMetricName'. Adding more metrics (e.g., workqueue depth)
	//   directly here risks turning Instrument into a "god object".
	//
	// Proposed patterns:
	//   (A) Metric Specs (data-driven):
	//       - Define []CounterDeltaSpec{Name, Key}
	//       - Store start snapshots in map[name]int64
	//       - End computes delta (end-start) for each spec
	//       Best when logic is identical (simple counters).
	//
	//   (B) Pluggable Collectors (interface-driven):
	//       - Define a Collector interface for metrics with different logic (gauge/histogram, etc.)
	//         e.g. Start(text) error; End(text) (key string, val *float64, err error)
	//       - Instrument owns lifecycle + best-effort policy, collectors own parsing/logic.
	//       Best when metrics require different extraction logic.
	//
	// Integration note:
	//   - Summary output may need a flexible container for dynamic metrics
	//     (e.g., SummaryMetrics.Extra map[string]*float64, or Summary.Extras).
	//   - Keep strict dependency boundaries: no k8s/controller-runtime imports in this package.
	reconcileTotalMetricName string

	// last error context (optional)
	lastFetchErr error
	lastParseErr error
}

type Option func(*Instrument)

// WithReconcileTotalMetricName overrides the default metric name.
// TODO(next): replace single metric override with multi-spec counters or measurement plugins.
func WithReconcileTotalMetricName(name string) Option {
	return func(i *Instrument) {
		if strings.TrimSpace(name) != "" {
			i.reconcileTotalMetricName = name
		}
	}
}

func WithEnabled(v bool) Option {
	return func(i *Instrument) {
		i.enabled = v
	}
}

// New creates a unified instrument.
// It checks slo.Enabled() automatically.
//
// NOTE: Keep this package "test oriented":
// - Do NOT import kubernetes/client-go/controller-runtime here.
// - Fetcher is injected from tests.
func New(labels slo.Labels, fetcher MetricsFetcher, logger slo.Logger, writer slo.SummaryWriter, opts ...Option) *Instrument {
	i := &Instrument{
		enabled:                  false, // default off
		fetcher:                  fetcher,
		labels:                   labels,
		logf:                     func(string, ...any) {},
		writer:                   writer,
		reconcileTotalMetricName: "controller_runtime_reconcile_total",
	}

	if logger != nil {
		i.logf = logger.Logf
	}

	for _, opt := range opts {
		if opt != nil {
			opt(i)
		}
	}

	return i
}

// Start records the start time and initial metrics snapshot.
// Best-effort: any failure means we skip delta later.
func (i *Instrument) Start(ctx context.Context) {
	if !i.enabled {
		return
	}
	i.startTime = time.Now()
	i.hasStart = true

	// Snapshot reconcile_total at start (best-effort)
	if i.fetcher == nil {
		i.logf("[slo-lab] Start: missing fetcher (skip metrics delta)")
		return
	}

	text, err := i.fetcher(ctx)
	if err != nil {
		i.lastFetchErr = err
		i.logf("[slo-lab] Start: fetch failed (skip metrics delta): %v", err)
		return
	}

	v, err := parseCounterSum(text, i.reconcileTotalMetricName)
	if err != nil {
		i.lastParseErr = err
		i.logf("[slo-lab] Start: parse failed (skip metrics delta): %v", err)
		return
	}

	i.reconcileStart = v
	i.hasRecStart = true
	i.logf("[slo-lab] Start: %s=%d", i.reconcileTotalMetricName, v)
}

// End records the end time, final metrics snapshot, and writes the summary.
// testPassed: result of the actual test assertion.
func (i *Instrument) End(ctx context.Context, testPassed bool) {
	if !i.enabled {
		return
	}
	now := time.Now()

	// 1) Convergence time (best-effort)
	var convSeconds *float64
	if i.hasStart {
		d := now.Sub(i.startTime).Seconds()
		convSeconds = &d
	} else {
		i.logf("[slo-lab] End: missing start time (skip convergence)")
	}

	// 2) Reconcile delta (best-effort)
	var deltaVal *float64
	if i.hasRecStart && i.fetcher != nil {
		text, err := i.fetcher(ctx)
		if err != nil {
			i.lastFetchErr = err
			i.logf("[slo-lab] End: fetch failed (skip delta): %v", err)
		} else {
			endV, err := parseCounterSum(text, i.reconcileTotalMetricName)
			if err != nil {
				i.lastParseErr = err
				i.logf("[slo-lab] End: parse failed (skip delta): %v", err)
			} else {
				d := float64(endV - i.reconcileStart)

				// [NEW] negative delta policy (common when process restarts / metrics reset)
				// - treat as missing => skip
				if d < 0 {
					i.logf("[slo-lab] End: negative delta=%s (skip delta)", fmt.Sprintf("%.0f", d))
				} else {
					deltaVal = &d
					i.logf("[slo-lab] End: %s end=%d delta=%s",
						i.reconcileTotalMetricName, endV, formatPtrFloat(deltaVal))
				}
			}
		}
	} else if !i.hasRecStart {
		i.logf("[slo-lab] End: missing start snapshot (skip delta)")
	}

	// 3) Determine Result (Rule B: measurement failure != test failure)
	finalResult := "success"
	if !testPassed {
		finalResult = "fail"
	} else if convSeconds == nil || deltaVal == nil {
		finalResult = "skip"
	}
	i.labels.Result = finalResult

	// 4) Write Summary (best-effort)
	if i.writer == nil {
		i.logf("[slo-lab] End: writer is nil (skip writing summary) result=%s conv=%s delta=%s",
			finalResult, formatPtrFloat(convSeconds), formatPtrFloat(deltaVal))
		return
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
		i.logf("[slo-lab] End: write summary failed (ignored): %v", err)
		return
	}

	i.logf("[slo-lab] wrote summary: result=%s conv=%s delta=%s",
		finalResult, formatPtrFloat(convSeconds), formatPtrFloat(deltaVal))
}

// parseCounterSum parses Prometheus text format and sums all series of a counter metric.
func parseCounterSum(text string, metricName string) (int64, error) {
	parser := expfmt.TextParser{}
	families, err := parser.TextToMetricFamilies(strings.NewReader(text))
	if err != nil {
		return 0, fmt.Errorf("text parse failed: %w", err)
	}

	mf, ok := families[metricName]
	if !ok || mf == nil {
		return 0, fmt.Errorf("metric not found: %s", metricName)
	}

	var sum float64
	for _, m := range mf.GetMetric() {
		// Prefer counter; tolerate gauge-like exposition in some cases.
		if c := m.GetCounter(); c != nil {
			sum += c.GetValue()
			continue
		}
		if g := m.GetGauge(); g != nil {
			sum += g.GetValue()
			continue
		}
	}

	return int64(sum), nil
}

// formatPtrFloat is a safe formatter for *float64.
// - nil => "nil"
// - non-nil => "%.6f"
func formatPtrFloat(p *float64) string {
	if p == nil {
		return "nil"
	}
	return fmt.Sprintf("%.6f", *p)
}
