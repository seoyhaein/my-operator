package instrumentv2

import "context"

// MetricMap is a parsed snapshot of Prometheus text format metrics.
// Key is a "series identity" string. In v2 we keep it simple: key = full series line identity,
// e.g. `metric_name{label="value",...}` or just `metric_name` when no labels.
type MetricMap map[string]float64

// Fetcher returns a structured metric snapshot. No K8s deps here.
type Fetcher func(context.Context) (MetricMap, error)

// Labels are Prometheus-friendly labels (intended to remain relatively low cardinality).
// (You can still keep run_id here if you have reasons.)
type Labels map[string]string

// SessionMeta is report-only metadata (safe to be high cardinality).
type SessionMeta struct {
	Method string // ginkgo|cli|watch
	Scope  string // test-run|time-window|resource
	RunID  string

	Suite     string
	TestCase  string
	Namespace string
}

// SummaryWriter persists a session result (JSON file, stdout, etc.).
type SummaryWriter interface {
	Save(ctx context.Context, session SessionResult) error
}

type MetricScope string

const (
	ScopeGlobal     MetricScope = "global"
	ScopeNamespaced MetricScope = "namespaced"
)

// MetricDef declares which metric (or series identity) is used for evaluation.
// NOTE: Name is matched against keys in MetricMap.
// - If you store keys as full series (metric{labels}), Name must match that exact key.
// - If you store keys as plain metric name, Name should be that metric name.
type MetricDef struct {
	Name  string
	Scope MetricScope
}

// EvaluationPolicy controls behavior in parallel execution.
type EvaluationPolicy struct {
	AllowParallel      bool
	OnGlobalInParallel string // "skip"|"warn"|"fail"
}

// SessionResult is a single measurement session output.
type SessionResult struct {
	Meta   SessionMeta
	Labels Labels

	// Wall-clock window for the measurement session.
	StartTimeUnixMs int64
	EndTimeUnixMs   int64

	Measurements map[string]float64
	Skipped      map[string]string // metricName -> reason
	Warnings     []string
	Errors       []string // optional: non-fatal errors to record in report
}
