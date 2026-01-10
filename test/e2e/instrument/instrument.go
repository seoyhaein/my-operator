package instrument

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"time"

	dto "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/expfmt"
	"github.com/yeongki/my-operator/pkg/slo"
)

type Result string

const (
	ResultSuccess Result = "success"
	ResultFail    Result = "fail"
	ResultSkip    Result = "skip"
)

type Instrument struct {
	enabled bool

	metricsURL string
	httpClient *http.Client

	logf   func(string, ...any)
	writer slo.SummaryWriter

	labels    slo.Labels
	startTime time.Time
	hasStart  bool

	reconcileStart int64
	hasRecStart    bool
}

type Option func(*Instrument)

func WithLogger(l slo.Logger) Option {
	return func(i *Instrument) { i.logf = func(format string, args ...any) { l.Logf(format, args...) } }
}

func WithWriter(w slo.SummaryWriter) Option {
	return func(i *Instrument) { i.writer = w }
}

func WithHTTPClient(c *http.Client) Option {
	return func(i *Instrument) { i.httpClient = c }
}

// New creates a glue-layer instrument.
// - env reading happens ONLY here (glue), not in pkg/slo.
func New(labels slo.Labels, metricsURL string, opts ...Option) *Instrument {
	i := &Instrument{
		enabled:    os.Getenv("SLO_ENABLED") == "1", // 문서의 opt-in 이름을 여기서만 결정
		metricsURL: metricsURL,
		httpClient: &http.Client{Timeout: 2 * time.Second},
		logf:       func(string, ...any) {},
		labels:     labels,
	}
	for _, o := range opts {
		o(i)
	}
	return i
}

func (i *Instrument) Enabled() bool { return i.enabled }

func (i *Instrument) Start(now time.Time) {
	if !i.enabled {
		return
	}
	i.startTime = now
	i.hasStart = true

	// 시작 스냅샷: controller_runtime_reconcile_total
	v, err := i.scrapeCounter(context.Background(), "controller_runtime_reconcile_total",
		map[string]string{
			// controller label은 네 프로젝트에 맞춰 주입 가능 (예: joboperator)
			// 필요 없으면 이 map을 비워도 됨. 대신 여러 컨트롤러 합산될 수 있음.
			"controller": "joboperator",
		},
	)
	if err != nil {
		i.logf("[slo-lab] start: skip reconcile snapshot: %v", err)
		return
	}

	i.reconcileStart = v
	i.hasRecStart = true
	i.logf("[slo-lab] start: reconcile_total=%d", v)
}

func (i *Instrument) End(now time.Time, testPassed bool) {
	if !i.enabled {
		return
	}

	// 1) result 라벨 결정 (테스트 결과가 우선)
	result := string(ResultSuccess)
	if !testPassed {
		result = string(ResultFail)
	}
	labels := i.labels
	labels.Result = result

	// 2) convergence 계산
	var convSeconds *float64
	if i.hasStart {
		d := now.Sub(i.startTime)
		if d >= 0 {
			v := d.Seconds()
			convSeconds = &v
		} else {
			i.logf("[slo-lab] end: negative duration, skip convergence")
		}
	} else {
		i.logf("[slo-lab] end: missing startTime, skip convergence")
	}

	// 3) reconcile delta 계산
	var deltaVal *float64
	if i.hasRecStart {
		endV, err := i.scrapeCounter(context.Background(), "controller_runtime_reconcile_total",
			map[string]string{"controller": "joboperator"},
		)
		if err != nil {
			i.logf("[slo-lab] end: skip reconcile snapshot(end): %v", err)
		} else {
			delta := endV - i.reconcileStart
			if delta >= 0 {
				f := float64(delta)
				deltaVal = &f
			} else {
				i.logf("[slo-lab] end: negative delta=%d, skip", delta)
			}
		}
	} else {
		i.logf("[slo-lab] end: missing reconcileStart, skip delta")
	}

	// 4) “측정 실패 ≠ 테스트 실패” 규칙 적용:
	// 테스트는 성공했지만 계측이 하나라도 안 됐으면 result=skip 으로 기록(문서 규칙)
	if testPassed {
		if convSeconds == nil || deltaVal == nil {
			labels.Result = string(ResultSkip)
		}
	}

	// 5) summary 작성 (writer 실패는 로그만)
	s := slo.Summary{
		Labels:    labels,
		CreatedAt: time.Now().UTC(),
		Metrics: slo.SummaryMetrics{
			E2EConvergenceTimeSeconds: convSeconds,
			ReconcileTotalDelta:       deltaVal,
		},
	}

	if i.writer != nil {
		if err := i.writer.WriteSummary(s); err != nil {
			i.logf("[slo-lab] write summary failed (ignored): %v", err)
		}
	}
}

// scrapeCounter scrapes Prometheus text exposition and extracts a counter value.
// labelMatch: metric의 label key/value가 모두 일치하는 항목을 고름.
//   - 매칭되는 항목이 여러 개면 “합산”해버리면 위험하니 여기서는 에러로 둔다.
//     (원하면 나중에 “합산 모드” 옵션을 추가)
func (i *Instrument) scrapeCounter(ctx context.Context, metricName string, labelMatch map[string]string) (int64, error) {
	if i.metricsURL == "" {
		return 0, errors.New("metricsURL is empty")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, i.metricsURL, nil)
	if err != nil {
		return 0, err
	}

	resp, err := i.httpClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode/100 != 2 {
		return 0, fmt.Errorf("metrics http status=%s", resp.Status)
	}

	var parser expfmt.TextParser
	families, err := parser.TextToMetricFamilies(resp.Body)
	if err != nil {
		return 0, fmt.Errorf("parse metrics: %w", err)
	}

	mf, ok := families[metricName]
	if !ok || mf == nil {
		return 0, fmt.Errorf("metric not found: %s", metricName)
	}

	// counter는 dto.Metric의 Counter.GetValue()를 본다
	var matched []*dto.Metric
	for _, m := range mf.Metric {
		if labelsMatch(m, labelMatch) {
			matched = append(matched, m)
		}
	}

	if len(matched) == 0 {
		return 0, fmt.Errorf("metric found but no series matched labels: %s %+v", metricName, labelMatch)
	}
	if len(matched) > 1 {
		return 0, fmt.Errorf("multiple series matched labels: %s %+v (n=%d)", metricName, labelMatch, len(matched))
	}

	c := matched[0].GetCounter()
	if c == nil {
		// controller_runtime_reconcile_total 는 counter가 맞아야 정상
		return 0, fmt.Errorf("metric is not counter: %s", metricName)
	}
	v := c.GetValue()
	if v < 0 {
		return 0, fmt.Errorf("counter negative: %f", v)
	}
	return int64(v), nil
}

func labelsMatch(m *dto.Metric, want map[string]string) bool {
	if len(want) == 0 {
		return true
	}
	got := map[string]string{}
	for _, lp := range m.Label {
		got[lp.GetName()] = lp.GetValue()
	}
	for k, v := range want {
		if got[k] != v {
			return false
		}
	}
	return true
}
