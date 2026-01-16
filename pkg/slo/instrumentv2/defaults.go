package instrumentv2

import "strings"

// DefaultTestCase returns testCase if non-empty, otherwise fallback.
func DefaultTestCase(testCase, fallback string) string {
	if strings.TrimSpace(testCase) != "" {
		return strings.TrimSpace(testCase)
	}
	return strings.TrimSpace(fallback)
}

// DefaultLabels builds low-cardinality labels for SessionResult.
func DefaultLabels(suite, testCase, namespace, runID string) Labels {
	ls := Labels{}
	if suite != "" {
		ls["suite"] = suite
	}
	if testCase != "" {
		ls["test_case"] = testCase
	}
	if namespace != "" {
		ls["namespace"] = namespace
	}
	if runID != "" {
		ls["run_id"] = runID
	}
	return ls
}

// DefaultMeta builds report-only metadata.
func DefaultMeta(method, scope, runID, suite, testCase, namespace string) SessionMeta {
	return SessionMeta{
		Method:    method,
		Scope:     scope,
		RunID:     runID,
		Suite:     suite,
		TestCase:  testCase,
		Namespace: namespace,
	}
}
