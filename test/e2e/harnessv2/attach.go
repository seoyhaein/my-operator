package harnessv2

import (
	"context"
	"fmt"
	"strings"

	. "github.com/onsi/ginkgo/v2"

	instrumentv2 "github.com/yeongki/my-operator/pkg/slo/instrumentv2"
)

// Attach registers BeforeEach/AfterEach hooks to automatically measure SLO for each test.
// - It does NOT read env vars.
// - It does NOT know how to obtain token.
// - It relies on depsProvider closure to supply per-test deps (token/runid/testcase/etc).
//
// Typical usage from test/e2e/e2e_test.go:
//
//	BeforeEach(setupToken)
//	Attach(func() Deps { return Deps{Token: token, ...} }, fns)
func Attach(depsProvider func() Deps, fns CurlPodFns) {
	var inst *instrumentv2.Instrument
	var enabled bool

	BeforeEach(func() {
		deps := depsProvider()
		enabled = deps.Enabled
		if !enabled {
			inst = nil
			return
		}

		// Auto-fill TestCase if empty.
		if strings.TrimSpace(deps.TestCase) == "" {
			deps.TestCase = CurrentSpecReport().LeafNodeText
		}

		inst = NewMetricsHarness(deps, fns)

		if err := inst.Start(context.Background()); err != nil {
			// Per your philosophy: instrumentation failure should NOT fail the test.
			_, _ = fmt.Fprintf(GinkgoWriter, "SLO(v2): Start failed (skip): %v\n", err)
			// Still keep inst to attempt End() best-effort; it will error if Start missing.
		}
	})

	AfterEach(func() {
		if !enabled || inst == nil {
			return
		}
		if err := inst.End(context.Background()); err != nil {
			// Per your philosophy: instrumentation failure should NOT fail the test.
			_, _ = fmt.Fprintf(GinkgoWriter, "SLO(v2): End failed (skip): %v\n", err)
		}
	})
}
