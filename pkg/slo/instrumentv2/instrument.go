package instrumentv2

import (
	"context"
	"fmt"
	"time"
)

type Instrument struct {
	labels  Labels
	meta    SessionMeta
	fetcher Fetcher
	writer  SummaryWriter

	defs   []MetricDef
	policy EvaluationPolicy

	startSnap MetricMap
	startAt   time.Time
}

func New(labels Labels, meta SessionMeta, fetcher Fetcher, w SummaryWriter, defs []MetricDef, pol EvaluationPolicy) *Instrument {
	return &Instrument{
		labels:  labels,
		meta:    meta,
		fetcher: fetcher,
		writer:  w,
		defs:    defs,
		policy:  pol,
	}
}

// Start snapshots the "before" metrics.
// NOTE: Per your philosophy, caller decides whether this error should fail the test.
// Typically you log+skip.
func (i *Instrument) Start(ctx context.Context) error {
	m, err := i.fetcher(ctx)
	if err != nil {
		return err
	}
	i.startSnap = m
	i.startAt = time.Now()
	return nil
}

// End snapshots the "after" metrics, computes deltas, applies parallel policy,
// and writes a SessionResult via SummaryWriter (if provided).
func (i *Instrument) End(ctx context.Context) error {
	if i.startSnap == nil {
		return fmt.Errorf("instrument: Start() must be called before End()")
	}

	endSnap, err := i.fetcher(ctx)
	if err != nil {
		return err
	}
	endAt := time.Now()

	res := SessionResult{
		Meta:            i.meta,
		Labels:          i.labels,
		StartTimeUnixMs: i.startAt.UnixMilli(),
		EndTimeUnixMs:   endAt.UnixMilli(),
		Measurements:    map[string]float64{},
		Skipped:         map[string]string{},
		Warnings:        []string{},
		Errors:          []string{},
	}

	for _, d := range i.defs {
		vStart, okS := i.startSnap[d.Name]
		vEnd, okE := endSnap[d.Name]
		if !okS || !okE {
			res.Skipped[d.Name] = "metric missing"
			continue
		}

		// Parallel policy for global metrics
		if i.policy.AllowParallel && d.Scope == ScopeGlobal {
			switch i.policy.OnGlobalInParallel {
			case "skip":
				res.Skipped[d.Name] = "global metric in parallel mode"
				continue
			case "warn":
				res.Warnings = append(res.Warnings, "global metric used in parallel mode: "+d.Name)
			case "fail":
				// Fail can be interpreted in two ways:
				// 1) return error here (hard fail)
				// 2) record as Errors and let CI judge based on report
				// Here we choose (2) to avoid coupling to test outcome.
				res.Errors = append(res.Errors, "policy fail: global metric in parallel mode: "+d.Name)
			default:
				// Unknown policy -> warn and proceed
				res.Warnings = append(res.Warnings, "unknown OnGlobalInParallel policy; proceeding: "+i.policy.OnGlobalInParallel)
			}
		}

		res.Measurements[d.Name] = vEnd - vStart
	}

	if i.writer == nil {
		return nil
	}
	return i.writer.Save(ctx, res)
}
