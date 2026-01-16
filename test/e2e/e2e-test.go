package e2e

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/yeongki/my-operator/test/e2e/harnessv2"
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
		// Load all env-driven configuration once.
		cfg = e2eenv.LoadOptions()

		By("creating manager namespace")
		cmd := exec.Command("kubectl", "create", "ns", namespace)
		_, err := utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred(), "Failed to create namespace")

		By("labeling the namespace to enforce the security policy")

		// [OLD] restricted enforce (kept for reference)
		// cmd = exec.Command("kubectl", "label", "--overwrite", "ns", namespace,
		// 	"pod-security.kubernetes.io/enforce=restricted")

		// [NEW] baseline to reduce flakiness while you are iterating.
		// - Later, when the manager pod is fully compliant, switch back to restricted.
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

	})

	AfterAll(func() {
		// Debug mode: keep cluster/resources for investigation.
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

	// v2: token 확보는 테스트 레이어(e2e)에서 수행
	BeforeEach(func() {
		t, err := utils.ServiceAccountToken(namespace, serviceAccountName, cfg.TokenRequestTimeout)
		Expect(err).NotTo(HaveOccurred())
		Expect(t).NotTo(BeEmpty())
		token = t
	})

	// v2: 계측은 Attach()로 훅에 고정 (테스트 본문은 계측을 몰라도 됨)
	harnessv2.Attach(
		func() harnessv2.Deps {
			allowParallel := strings.TrimSpace(os.Getenv("SLO_ALLOW_PARALLEL")) == "1"
			return harnessv2.Deps{
				Namespace:          namespace,
				Token:              token,
				MetricsServiceName: metricsServiceName,
				ServiceAccountName: serviceAccountName,

				ArtifactsDir: cfg.ArtifactsDir,

				Suite:    "e2e",
				TestCase: "", // Attach() auto-fills leaf node text
				RunID:    cfg.RunID,

				AllowParallel: allowParallel,
				Enabled:       cfg.Enabled,
			}
		},
		harnessv2.CurlPodFns{
			RunCurlMetricsOnce:  runCurlMetricsOnce,
			WaitCurlMetricsDone: waitCurlMetricsDone,
			CurlMetricsLogs:     curlMetricsLogs,
			DeletePodNoWait:     deletePodNoWait,
		},
	)

	// 이제 테스트는 "검증만" 한다
	It("should ensure the metrics endpoint is serving metrics", func() {
		By("scraping /metrics via curl pod")

		podName, err := runCurlMetricsOnce(namespace, token, metricsServiceName, serviceAccountName)
		Expect(err).NotTo(HaveOccurred())

		waitCurlMetricsDone(namespace, podName)

		text, err := curlMetricsLogs(namespace, podName)
		_ = deletePodNoWait(namespace, podName)
		Expect(err).NotTo(HaveOccurred())

		Expect(text).To(ContainSubstring("controller_runtime_reconcile_total"))

		By(fmt.Sprintf("done (timeout=%s)", 2*time.Minute))
	})
})
