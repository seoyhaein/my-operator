package harnessv2

import (
	"bufio"
	"context"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	instrumentv2 "github.com/yeongki/my-operator/pkg/slo/instrumentv2"
)

// Deps are the minimum inputs needed to build a metrics harness.
type Deps struct {
	Namespace          string
	Token              string
	MetricsServiceName string
	ServiceAccountName string

	ArtifactsDir string

	Suite    string
	TestCase string
	RunID    string

	AllowParallel bool
	Enabled       bool
}

// CurlPodFns are injected to avoid import cycles (harnessv2 should not import test/e2e directly).
type CurlPodFns struct {
	RunCurlMetricsOnce  func(ns, token, metricsSvc, sa string) (podName string, err error)
	WaitCurlMetricsDone func(ns, podName string)
	CurlMetricsLogs     func(ns, podName string) (string, error)
	DeletePodNoWait     func(ns, podName string) error
}

// NewMetricsHarness builds a v2 Instrument for "Ginkgo hook" measurement.
func NewMetricsHarness(deps Deps, fns CurlPodFns) *instrumentv2.Instrument {
	if !deps.Enabled {
		// Not used in practice because Attach() already skips, but keep safe.
		return instrumentv2.New(nil, instrumentv2.SessionMeta{}, func(context.Context) (instrumentv2.MetricMap, error) {
			return instrumentv2.MetricMap{}, nil
		}, nil, nil, instrumentv2.EvaluationPolicy{})
	}

	// Fetcher: run curl pod -> get raw text -> parse
	fetcher := func(ctx context.Context) (instrumentv2.MetricMap, error) {
		podName, err := fns.RunCurlMetricsOnce(deps.Namespace, deps.Token, deps.MetricsServiceName, deps.ServiceAccountName)
		if err != nil {
			return nil, err
		}
		fns.WaitCurlMetricsDone(deps.Namespace, podName)

		raw, err := fns.CurlMetricsLogs(deps.Namespace, podName)
		_ = fns.DeletePodNoWait(deps.Namespace, podName)
		if err != nil {
			return nil, err
		}
		return ParsePrometheusText(raw), nil
	}

	labels := instrumentv2.Labels{
		"suite":     deps.Suite,
		"test_case": deps.TestCase,
		"namespace": deps.Namespace,
		"run_id":    deps.RunID, // per your current choice
	}

	meta := instrumentv2.SessionMeta{
		Method:    "ginkgo",
		Scope:     "test-run",
		RunID:     deps.RunID,
		Suite:     deps.Suite,
		TestCase:  deps.TestCase,
		Namespace: deps.Namespace,
	}

	defs := []instrumentv2.MetricDef{
		{Name: "controller_runtime_reconcile_total", Scope: instrumentv2.ScopeGlobal},
	}

	pol := instrumentv2.EvaluationPolicy{
		AllowParallel:      deps.AllowParallel,
		OnGlobalInParallel: "skip",
	}

	var w instrumentv2.SummaryWriter
	if deps.ArtifactsDir != "" {
		filename := fmt.Sprintf(
			"sli-summary.%s.%s.json",
			instrumentv2.SanitizeFilename(deps.RunID),
			instrumentv2.SanitizeFilename(deps.TestCase),
		)
		w = instrumentv2.NewJSONFileWriter(filepath.Join(deps.ArtifactsDir, filename))
	}

	return instrumentv2.New(labels, meta, fetcher, w, defs, pol)
}

// ParsePrometheusText parses Prometheus text format into MetricMap.
// It supports lines like:
//
//	metric_name 123
//	metric_name{label="value",...} 123
func ParsePrometheusText(raw string) instrumentv2.MetricMap {
	out := instrumentv2.MetricMap{}
	sc := bufio.NewScanner(strings.NewReader(raw))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		series := fields[0]
		valStr := fields[1]
		v, err := strconv.ParseFloat(valStr, 64)
		if err != nil {
			continue
		}
		if strings.Contains(series, "{") {
			out[series] = v
			plain := series[:strings.Index(series, "{")]
			out[plain] = out[plain] + v
			continue
		}
		out[series] = v
	}
	return out
}
