package utils

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// TODO(util): TokenRequest generalization for reuse across operators.
//
// Current behavior:
//   - TokenRequest body omits spec.audiences / spec.expirationSeconds (cluster defaults).
//   - Works well for this test environment, but may be environment-/receiver-dependent.
//
// Risks when used as a shared utility:
//   - Default audience differs by cluster / distro; some receivers (proxy/auth middleware)
//     may enforce audience strictly -> 401 even with a valid token.
//   - Some apiservers may require spec/audiences (schema/validation differences) -> 4xx on token request.
//   - Short expiration can introduce flaky failures during long debug sessions; long expiration may be preferred.
//
// Follow-up work:
//   1) Expose token request options (escape hatch):
//        - Audiences []string (nil = omit / default behavior)
//        - ExpirationSeconds *int64 (nil = omit / default)
//        - Optional BoundObjectRef / raw TokenRequest override for advanced setups
//   2) Implement audience fallback strategy for robustness:
//        - Try nil (omit) first
//        - If metrics access returns 401/403 and/or token request fails with validation,
//          retry with common candidates (e.g. "https://kubernetes.default.svc", "kubernetes")
//        - Allow caller-provided preferred audiences list
//   3) Improve diagnostics:
//        - Include which audience candidate was used
//        - Distinguish "token creation failed" vs "metrics fetch failed" vs "network/tls issues"
//        - Preserve stderr in error messages (already partially done in utils.Run)
//   4) Add tests (table-driven):
//        - "no auth" metrics endpoint
//        - "auth required" endpoint with strict audience checking (if available via test fixture)
//        - long-running test to ensure expiration doesn't cause flakes

// serviceAccountToken returns a token for the specified service account in the given namespace.
// Token request helper (FIX: avoid CombinedOutput for JSON parsing)

func ServiceAccountToken(ns, sa string, timeout time.Duration) (string, error) {
	const tokenRequestRawString = `{
  "apiVersion": "authentication.k8s.io/v1",
  "kind": "TokenRequest"
}`

	var out string
	var lastErr error

	verifyTokenCreation := func(g Gomega) {
		cmd := exec.Command("kubectl", "create", "--raw", fmt.Sprintf(
			"/api/v1/namespaces/%s/serviceaccounts/%s/token",
			ns, sa,
		), "-f", "-")

		cmd.Stdin = strings.NewReader(tokenRequestRawString)

		stdout, err := Run(cmd)
		if err != nil {
			lastErr = fmt.Errorf("token request failed (ns=%s sa=%s): %w", ns, sa, err)
			g.Expect(err).NotTo(HaveOccurred())
			return
		}

		var token tokenRequest
		if err := json.Unmarshal([]byte(stdout), &token); err != nil {
			lastErr = fmt.Errorf("token response json parse failed (ns=%s sa=%s): %w (body=%q)", ns, sa, err, stdout)
			g.Expect(err).NotTo(HaveOccurred())
			return
		}

		out = token.Status.Token
		g.Expect(out).NotTo(BeEmpty(), "token is empty")
	}

	Eventually(verifyTokenCreation, timeout, 2*time.Second).Should(Succeed())

	if out == "" && lastErr != nil {
		return "", lastErr
	}
	return out, nil
}

type tokenRequest struct {
	Status struct {
		Token string `json:"token"`
	} `json:"status"`
}

// summaryWriterFromEnv Summary writer helper
//func summaryWriterFromEnv() artifacts.JSONFileWriter {
//	dir := os.Getenv("ARTIFACTS_DIR")
//	if dir == "" {
//		dir = "/tmp"
//	}
//	return artifacts.JSONFileWriter{
//		Path: filepath.Join(dir, "sli-summary.json"),
//	}
//}

// applyClusterRoleBinding Idempotent ClusterRoleBinding helper
func applyClusterRoleBinding(name, clusterRole, ns, sa string) error {
	yaml := fmt.Sprintf(`apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: %s
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: %s
subjects:
- kind: ServiceAccount
  name: %s
  namespace: %s
`, name, clusterRole, sa, ns)

	cmd := exec.Command("kubectl", "apply", "-f", "-")
	cmd.Stdin = strings.NewReader(yaml)

	out, err := cmd.CombinedOutput()
	_, _ = fmt.Fprintf(GinkgoWriter, "running: %q\n", strings.Join(cmd.Args, " "))
	if len(out) > 0 {
		_, _ = fmt.Fprintf(GinkgoWriter, "%s\n", string(out))
	}
	if err != nil {
		return fmt.Errorf("kubectl apply clusterrolebinding failed: %w", err)
	}
	return nil
}

// -----------------------------------------------------------------------------
// Logger adapter for instrument
// -----------------------------------------------------------------------------

type testLogger struct{}

func (t *testLogger) Logf(format string, args ...any) {
	_, _ = fmt.Fprintf(GinkgoWriter, format+"\n", args...)
}
