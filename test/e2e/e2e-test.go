package e2e

import (
	"fmt"
	"os/exec"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/yeongki/my-operator/test/e2e/harness"
	e2eenv "github.com/yeongki/my-operator/test/e2e/internal/env"
	"github.com/yeongki/my-operator/test/utils"
)

const namespace = "my-operator-system"
const serviceAccountName = "my-operator-controller-manager"
const metricsServiceName = "my-operator-controller-manager-metrics-service"

var _ = Describe("Manager", Ordered, func() {
	var (
		cfg   e2eenv.Options
		token string
	)

	BeforeAll(func() {
		cfg = e2eenv.LoadOptions()
		By(fmt.Sprintf("ArtifactsDir=%q RunID=%q Enabled=%v", cfg.ArtifactsDir, cfg.RunID, cfg.Enabled))

		By("creating manager namespace")
		cmd := exec.Command("kubectl", "create", "ns", namespace)
		_, err := utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred(), "Failed to create namespace")

		By("labeling the namespace to enforce the security policy")
		cmd = exec.Command("kubectl", "label", "--overwrite", "ns", namespace,
			"pod-security.kubernetes.io/enforce=baseline")
		_, err = utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred(), "Failed to label namespace with security policy")

		By("installing CRDs")
		cmd = exec.Command("make", "install")
		_, err = utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred(), "Failed to install CRDs")

		By("deploying the controller-manager")
		cmd = exec.Command("make", "deploy", fmt.Sprintf("IMG=%s", projectImage))
		_, err = utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred(), "Failed to deploy the controller-manager")

		By("ensuring metrics reader RBAC for controller-manager SA")
		cmd = exec.Command("bash", "-lc", fmt.Sprintf(`
set -euo pipefail
kubectl create clusterrolebinding my-operator-e2e-metrics-reader \
  --clusterrole=my-operator-metrics-reader \
  --serviceaccount=%s:%s \
  --dry-run=client -o yaml | kubectl apply -f -
`, namespace, serviceAccountName))
		_, err = utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred())
	})

	AfterAll(func() {
		if cfg.SkipCleanup {
			By("E2E_SKIP_CLEANUP enabled: skipping cleanup")
			return
		}

		By("best-effort: cleaning up curl-metrics pods for metrics")
		cleanupCurlMetricsPods(namespace)

		By("undeploying the controller-manager")
		cmd := exec.Command("make", "undeploy")
		_, _ = utils.Run(cmd)

		By("uninstalling CRDs")
		cmd = exec.Command("make", "uninstall")
		_, _ = utils.Run(cmd)

		By("removing manager namespace")
		cmd = exec.Command("kubectl", "delete", "ns", namespace)
		_, _ = utils.Run(cmd)
	})

	BeforeEach(func() {
		By("requesting service account token")
		t, err := utils.ServiceAccountToken(namespace, serviceAccountName, cfg.TokenRequestTimeout)
		Expect(err).NotTo(HaveOccurred())
		Expect(t).NotTo(BeEmpty())
		token = t

		By("waiting controller-manager ready")
		waitControllerManagerReady(namespace)

		By("waiting metrics service endpoints ready")
		waitServiceHasEndpoints(namespace, metricsServiceName)
	})

	harness.Attach(
		func() harness.HarnessDeps {
			return harness.HarnessDeps{
				ArtifactsDir: cfg.ArtifactsDir,
				Suite:        "e2e",
				TestCase:     "",
				RunID:        cfg.RunID,
				Enabled:      cfg.Enabled,
			}
		},
		func() harness.FetchDeps {
			return harness.FetchDeps{
				Namespace:          namespace,
				Token:              token,
				MetricsServiceName: metricsServiceName,
				ServiceAccountName: serviceAccountName,
			}
		},
		harness.DefaultV3Specs, // ✅ e2e에서 spec 패키지 import 없이
		harness.CurlPodFns{
			RunCurlMetricsOnce:  runCurlMetricsOnce,
			WaitCurlMetricsDone: waitCurlMetricsDone,
			CurlMetricsLogs:     curlMetricsLogs,
			DeletePodNoWait:     deletePodNoWait,
		},
	)

	It("should ensure the metrics endpoint is serving metrics", func() {
		By("scraping /metrics via curl pod")

		podName, err := runCurlMetricsOnce(namespace, token, metricsServiceName, serviceAccountName)
		Expect(err).NotTo(HaveOccurred())

		waitCurlMetricsDone(namespace, podName)

		text, err := curlMetricsLogs(namespace, podName)
		_ = deletePodNoWait(namespace, podName)
		Expect(err).NotTo(HaveOccurred())

		if !strings.Contains(text, "controller_runtime_reconcile_total") {
			head := text
			if len(head) > 800 {
				head = head[:800]
			}
			GinkgoWriter.Printf("metrics text head:\n%s\n", head)
		}

		Expect(text).To(ContainSubstring("controller_runtime_reconcile_total"))

		By(fmt.Sprintf("done (timeout=%s)", 2*time.Minute))
	})
})
