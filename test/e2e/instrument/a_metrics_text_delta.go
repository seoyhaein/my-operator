package instrument

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type Status string

const (
	StatusPass Status = "pass"
	StatusSkip Status = "skip"
)

type Summary struct {
	RunID string `json:"run_id"`

	Status     Status `json:"status"`
	SkipReason string `json:"skip_reason,omitempty"`

	MetricsSnapshotStartAt string `json:"metrics_snapshot_start_at,omitempty"`
	MetricsSnapshotEndAt   string `json:"metrics_snapshot_end_at,omitempty"`

	ReconcileTotalStart float64 `json:"reconcile_total_start,omitempty"`
	ReconcileTotalEnd   float64 `json:"reconcile_total_end,omitempty"`
	ReconcileTotalDelta float64 `json:"reconcile_total_delta,omitempty"`
}

// ADeltaInstrument : A 옵션(텍스트 기반) 최소 계측기.
// - /metrics 텍스트(=curl pod logs에서 얻은 body 포함)를 입력으로 받음
// - controller_runtime_reconcile_total 모든 시계열 값을 합산해서 start/end/delta 기록
// - 실패해도 테스트를 실패시키지 않음(Caller가 Skip 처리)
type ADeltaInstrument struct {
	Enabled bool

	ArtifactsDir string
	FileName     string // default: sli-summary.json

	Logf func(string, ...any)

	runID    string
	startAt  time.Time
	startV   float64
	hasStart bool
}

func NewADelta(enabled bool, artifactsDir string, logf func(string, ...any)) *ADeltaInstrument {
	return &ADeltaInstrument{
		Enabled:      enabled,
		ArtifactsDir: artifactsDir,
		FileName:     "sli-summary.json",
		Logf:         logf,
	}
}

func (i *ADeltaInstrument) StartFromMetricsText(text string) (skipReason string) {
	i.runID = time.Now().UTC().Format(time.RFC3339Nano)

	if !i.Enabled {
		return "instrumentation disabled"
	}
	i.startAt = time.Now()

	v, err := sumPromMetricFromCurlLogs(text, "controller_runtime_reconcile_total")
	if err != nil {
		i.debug("instrument(A): Start parse failed: %v", err)
		return fmt.Sprintf("start parse failed: %v", err)
	}

	i.startV = v
	i.hasStart = true
	i.debug("instrument(A): Start ok reconcile_total=%f", v)
	return ""
}

func (i *ADeltaInstrument) EndFromMetricsText(text string, startSkipReason string) {
	// Always best-effort write a summary file.
	sum := Summary{
		RunID: i.runID,
	}

	// If Start already decided to skip, keep it skip.
	if startSkipReason != "" {
		sum.Status = StatusSkip
		sum.SkipReason = startSkipReason
		sum.MetricsSnapshotStartAt = i.startAt.UTC().Format(time.RFC3339Nano)
		i.writeBestEffort(sum)
		return
	}

	if !i.hasStart {
		sum.Status = StatusSkip
		sum.SkipReason = "start snapshot missing"
		i.writeBestEffort(sum)
		return
	}

	endAt := time.Now()
	endV, err := sumPromMetricFromCurlLogs(text, "controller_runtime_reconcile_total")
	if err != nil {
		sum.Status = StatusSkip
		sum.SkipReason = fmt.Sprintf("end parse failed: %v", err)
		sum.MetricsSnapshotStartAt = i.startAt.UTC().Format(time.RFC3339Nano)
		sum.MetricsSnapshotEndAt = endAt.UTC().Format(time.RFC3339Nano)
		i.writeBestEffort(sum)
		return
	}

	sum.Status = StatusPass
	sum.MetricsSnapshotStartAt = i.startAt.UTC().Format(time.RFC3339Nano)
	sum.MetricsSnapshotEndAt = endAt.UTC().Format(time.RFC3339Nano)

	sum.ReconcileTotalStart = i.startV
	sum.ReconcileTotalEnd = endV
	sum.ReconcileTotalDelta = endV - i.startV

	i.writeBestEffort(sum)
}

func (i *ADeltaInstrument) writeBestEffort(sum Summary) {
	if !i.Enabled {
		// Even if disabled, we still can write a skip summary; 하지만 여기서는 호출자가 이미 skip reason 넣게 되어있음.
	}

	dir := i.ArtifactsDir
	if dir == "" {
		dir = os.Getenv("ARTIFACTS_DIR")
	}
	if dir == "" {
		dir = "./_artifacts"
	}

	if err := os.MkdirAll(dir, 0o755); err != nil {
		i.debug("instrument(A): mkdir failed (ignored): %v", err)
		return
	}

	name := i.FileName
	if name == "" {
		name = "sli-summary.json"
	}

	finalPath := filepath.Join(dir, name)
	tmpPath := finalPath + ".tmp"

	b, err := json.MarshalIndent(sum, "", "  ")
	if err != nil {
		i.debug("instrument(A): json marshal failed (ignored): %v", err)
		return
	}
	b = append(b, '\n')

	if err := os.WriteFile(tmpPath, b, 0o644); err != nil {
		i.debug("instrument(A): write tmp failed (ignored): %v", err)
		return
	}
	if err := os.Rename(tmpPath, finalPath); err != nil {
		_ = os.Remove(tmpPath)
		i.debug("instrument(A): rename failed (ignored): %v", err)
		return
	}

	i.debug("instrument(A): wrote %s (status=%s)", finalPath, sum.Status)
}

func (i *ADeltaInstrument) debug(format string, args ...any) {
	if i.Logf != nil {
		i.Logf(format, args...)
	}
}

// curl -v 로그에는 "< HTTP/1.1 200 OK" 같은 라인도 섞임.
// body(메트릭 텍스트)는 일반 텍스트로 섞여 들어오므로,
// "metricName{" 또는 "metricName " 로 시작하는 라인을 찾아 모든 시계열 value를 합산한다.
func sumPromMetricFromCurlLogs(curlLogs string, metricName string) (float64, error) {
	lines := strings.Split(curlLogs, "\n")
	prefix1 := metricName + "{"
	prefix2 := metricName + " "

	var sum float64
	found := false

	for _, raw := range lines {
		ln := strings.TrimSpace(raw)
		if ln == "" {
			continue
		}

		// curl -v header lines: "< ...", "> ..."
		ln = strings.TrimPrefix(ln, "< ")
		ln = strings.TrimPrefix(ln, "> ")
		ln = strings.TrimSpace(ln)

		if strings.HasPrefix(ln, "#") {
			continue
		}
		if strings.HasPrefix(ln, prefix1) || strings.HasPrefix(ln, prefix2) {
			fields := strings.Fields(ln)
			if len(fields) < 2 {
				continue
			}
			v, err := strconv.ParseFloat(fields[1], 64)
			if err != nil {
				continue
			}
			sum += v
			found = true
		}
	}

	if !found {
		return 0, fmt.Errorf("metric not found: %s", metricName)
	}
	return sum, nil
}
