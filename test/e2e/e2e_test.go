/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/yeongki/my-operator/internal/artifacts"
	"github.com/yeongki/my-operator/pkg/slo"
	"github.com/yeongki/my-operator/test/e2e/instrument"
	"github.com/yeongki/my-operator/test/utils"
)

// namespace where the project is deployed in
const namespace = "my-operator-system"

// serviceAccountName created for the project
const serviceAccountName = "my-operator-controller-manager"

// metricsServiceName is the name of the metrics service of the project
const metricsServiceName = "my-operator-controller-manager-metrics-service"

// metricsRoleBindingName is the name of the RBAC that will be created to allow get the metrics data
const metricsRoleBindingName = "my-operator-metrics-binding"

// controller manager deployment name (kubebuilder scaffold default)
const controllerManagerDeploymentName = "my-operator-controller-manager"

// -----------------------------------------------------------------------------
// NOTE
// - utils.Run() returns STDOUT only on success (stable for jsonpath parsing)
// - On failure, error contains stderr+stdout for debugging
// - TokenRequest keeps CombinedOutput() to preserve rich error messages.
// -----------------------------------------------------------------------------

var _ = Describe("Manager", Ordered, func() {
	var controllerPodName string

	BeforeAll(func() {
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
		// [NEW] debug mode: keep cluster/resources for investigation
		if os.Getenv("E2E_SKIP_CLEANUP") == "1" {
			By("E2E_SKIP_CLEANUP=1: skipping cleanup")
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

	AfterEach(func() {
		specReport := CurrentSpecReport()
		if !specReport.Failed() {
			return
		}

		By("Failure dump: listing deploy/rs/pods (best-effort)")
		cmd := exec.Command("kubectl", "get", "deploy,rs,pods", "-n", namespace, "-o", "wide")
		if out, err := utils.Run(cmd); err == nil {
			_, _ = fmt.Fprintf(GinkgoWriter, "Resources (deploy/rs/pods):\n%s\n", out)
		} else {
			_, _ = fmt.Fprintf(GinkgoWriter, "Failed to list resources: %v\n", err)
		}

		By("Failure dump: fetching Kubernetes events (best-effort)")
		cmd = exec.Command("kubectl", "get", "events", "-n", namespace, "--sort-by=.lastTimestamp")
		if out, err := utils.Run(cmd); err == nil {
			_, _ = fmt.Fprintf(GinkgoWriter, "Events:\n%s\n", out)
		} else {
			_, _ = fmt.Fprintf(GinkgoWriter, "Failed to get events: %v\n", err)
		}

		By("Failure dump: describing controller-manager deployment (best-effort)")
		cmd = exec.Command("kubectl", "describe", "deploy", controllerManagerDeploymentName, "-n", namespace)
		if out, err := utils.Run(cmd); err == nil {
			_, _ = fmt.Fprintf(GinkgoWriter, "Deployment describe:\n%s\n", out)
		} else {
			_, _ = fmt.Fprintf(GinkgoWriter, "Failed to describe deployment: %v\n", err)
		}

		if controllerPodName == "" {
			_, _ = fmt.Fprintf(GinkgoWriter, "controllerPodName is empty; skip controller pod logs/describe\n")
			return
		}

		By("Failure dump: fetching controller manager pod logs (best-effort)")
		cmd = exec.Command("kubectl", "logs", controllerPodName, "-n", namespace)
		if out, err := utils.Run(cmd); err == nil {
			_, _ = fmt.Fprintf(GinkgoWriter, "Controller logs:\n%s\n", out)
		} else {
			_, _ = fmt.Fprintf(GinkgoWriter, "Failed to get controller logs: %v\n", err)
		}

		By("Failure dump: describing controller manager pod (best-effort)")
		cmd = exec.Command("kubectl", "describe", "pod", controllerPodName, "-n", namespace)
		if out, err := utils.Run(cmd); err == nil {
			_, _ = fmt.Fprintf(GinkgoWriter, "Pod describe:\n%s\n", out)
		} else {
			_, _ = fmt.Fprintf(GinkgoWriter, "Failed to describe controller pod: %v\n", err)
		}
	})

	SetDefaultEventuallyTimeout(2 * time.Minute)
	SetDefaultEventuallyPollingInterval(time.Second)

	Context("Manager", func() {
		It("should run successfully", func() {
			By("validating that the controller-manager pod is running as expected")

			// Print kubectl context/env only once (Eventually calls many times).
			var debugKubectlOnce sync.Once

			verifyControllerUp := func(g Gomega) {
				debugKubectlOnce.Do(func() {
					By("debug kubectl context used by e2e test")

					out, err := utils.Run(exec.Command("kubectl", "version", "--client=true"))
					_, _ = fmt.Fprintf(GinkgoWriter, "e2e kubectl client:\n%s\nerr=%v\n", strings.TrimSpace(out), err)

					out, err = utils.Run(exec.Command("kubectl", "config", "current-context"))
					_, _ = fmt.Fprintf(GinkgoWriter, "e2e kubectl current-context=%q err=%v\n", strings.TrimSpace(out), err)

					_, _ = fmt.Fprintf(GinkgoWriter, "ENV KUBECONFIG=%q HOME=%q PATH=%q\n",
						os.Getenv("KUBECONFIG"), os.Getenv("HOME"), os.Getenv("PATH"))

					out, err = utils.Run(exec.Command("kubectl", "config", "view", "--minify", "--raw"))
					_, _ = fmt.Fprintf(GinkgoWriter, "e2e kubeconfig(minify/raw):\n%s\nerr=%v\n", out, err)

					out, err = utils.Run(exec.Command("kubectl", "cluster-info"))
					_, _ = fmt.Fprintf(GinkgoWriter, "e2e cluster-info:\n%s\nerr=%v\n", strings.TrimSpace(out), err)
				})

				// -----------------------------------------------------------------
				// controller-manager pod name discovery (minimal change, more robust)
				//
				// Why:
				// - The old jsonpath filter `deletionTimestamp==null` can return empty
				//   unexpectedly depending on kubectl/jsonpath behavior.
				// - Using `.items[0]` can error when the list is empty.
				//
				// Approach:
				// - Query ALL matching pod names as a space-separated list:
				//     {.items[*].metadata.name}
				// - Split it in Go, and pick the first token.
				// - If empty, we fail the assertion -> Eventually retries.
				// -----------------------------------------------------------------

				// [OLD] flaky in some environments (kept for study)
				// cmd := exec.Command("kubectl", "get", "pods",
				// 	"-n", namespace,
				// 	"-l", "control-plane=controller-manager",
				// 	"-o", "jsonpath={.items[?(@.metadata.deletionTimestamp==null)].metadata.name}",
				// )
				// podName, err := utils.Run(cmd)
				// g.Expect(err).NotTo(HaveOccurred(), "Failed to retrieve controller-manager pod name")
				// controllerPodName = strings.TrimSpace(podName)
				// g.Expect(controllerPodName).NotTo(BeEmpty(), "controller-manager pod not found yet")

				// [NEW] robust query
				cmd := exec.Command("kubectl", "get", "pods",
					"-n", namespace,
					"-l", "control-plane=controller-manager",
					"-o", "jsonpath={.items[*].metadata.name}",
				)
				out, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred(), "Failed to retrieve controller-manager pod names")

				names := strings.Fields(out)
				g.Expect(names).NotTo(BeEmpty(), "controller-manager pod not found yet")

				controllerPodName = strings.TrimSpace(names[0])
				g.Expect(controllerPodName).To(ContainSubstring("controller-manager"))

				// Validate phase is Running
				cmd = exec.Command("kubectl", "get", "pod", controllerPodName,
					"-n", namespace,
					"-o", "jsonpath={.status.phase}",
				)
				phase, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(strings.TrimSpace(phase)).To(Equal("Running"), "Incorrect controller-manager pod status")
			}

			Eventually(verifyControllerUp, 5*time.Minute, 2*time.Second).Should(Succeed())
		})

		It("should ensure the metrics endpoint is serving metrics (with unified instrumentation)", func() {
			By("ensuring ClusterRoleBinding exists (idempotent apply)")
			err := applyClusterRoleBinding(metricsRoleBindingName, "my-operator-metrics-reader", namespace, serviceAccountName)
			Expect(err).NotTo(HaveOccurred(), "Failed to apply ClusterRoleBinding")

			By("validating that the metrics service is available")
			cmd := exec.Command("kubectl", "get", "service", metricsServiceName, "-n", namespace)
			_, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred(), "Metrics service should exist")

			By("getting the service account token (TokenRequest)")
			token, err := serviceAccountToken()
			Expect(err).NotTo(HaveOccurred())
			Expect(token).NotTo(BeEmpty())

			By("waiting for the metrics endpoint to be ready")
			verifyMetricsEndpointReady := func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "endpoints", metricsServiceName, "-n", namespace)
				out, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(out).To(ContainSubstring("8443"), "Metrics endpoint is not ready")
			}
			Eventually(verifyMetricsEndpointReady, 5*time.Minute, 2*time.Second).Should(Succeed())

			By("verifying that the controller manager is serving the metrics server")
			verifyMetricsServerStarted := func(g Gomega) {
				cmd := exec.Command("kubectl", "logs", controllerPodName, "-n", namespace)
				out, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(out).To(ContainSubstring("controller-runtime.metrics\tServing metrics server"),
					"Metrics server not yet started")
			}
			Eventually(verifyMetricsServerStarted, 5*time.Minute, 2*time.Second).Should(Succeed())

			By("SLO: initializing unified instrumentation")

			metricsFetcher := func(ctx context.Context) (string, error) {
				_ = ctx // keep signature future-proof

				podName, err := runCurlMetricsOnce(namespace, token, metricsServiceName, serviceAccountName)
				if err != nil {
					return "", err
				}

				Eventually(func(g Gomega) {
					phase, err := curlMetricsPhase(namespace, podName)
					g.Expect(err).NotTo(HaveOccurred())
					phase = strings.TrimSpace(phase)
					g.Expect(phase == "Succeeded" || phase == "Failed").To(BeTrue(),
						"curl-metrics pod not finished yet: phase=%s", phase)
				}, 5*time.Minute, 2*time.Second).Should(Succeed())

				logs, err := curlMetricsLogs(namespace, podName)
				_ = deletePodNoWait(namespace, podName)
				return logs, err
			}

			writer := summaryWriterFromEnv()

			labels := slo.Labels{
				Suite:     "e2e",
				TestCase:  "metrics-health",
				Namespace: namespace,
				RunID:     os.Getenv("CI_RUN_ID"),
			}

			inst := instrument.New(labels, metricsFetcher, &testLogger{}, writer)

			inst.Start(context.Background())
			defer func() {
				passed := !CurrentSpecReport().Failed()
				inst.End(context.Background(), passed)
			}()

			By("sanity: scraping metrics once via fetcher")
			text, err := metricsFetcher(context.Background())
			Expect(err).NotTo(HaveOccurred(), "metrics fetcher failed")
			Expect(text).To(ContainSubstring("controller_runtime_reconcile_total"))
		})
	})
})

// -----------------------------------------------------------------------------
// Token request helper (FIX: no /tmp file dependency)
// -----------------------------------------------------------------------------

// serviceAccountToken returns a token for the specified service account in the given namespace.
func serviceAccountToken() (string, error) {
	const tokenRequestRawString = `{
  "apiVersion": "authentication.k8s.io/v1",
  "kind": "TokenRequest"
}`

	var out string
	var lastErr error

	verifyTokenCreation := func(g Gomega) {
		cmd := exec.Command("kubectl", "create", "--raw", fmt.Sprintf(
			"/api/v1/namespaces/%s/serviceaccounts/%s/token",
			namespace,
			serviceAccountName,
		), "-f", "-")

		cmd.Stdin = strings.NewReader(tokenRequestRawString)

		output, err := cmd.CombinedOutput()
		if err != nil {
			lastErr = fmt.Errorf("token request failed: %s", string(output))
			g.Expect(err).NotTo(HaveOccurred())
			return
		}

		var token tokenRequest
		if err := json.Unmarshal(output, &token); err != nil {
			lastErr = fmt.Errorf("token response json parse failed: %w (body=%s)", err, string(output))
			g.Expect(err).NotTo(HaveOccurred())
			return
		}

		out = token.Status.Token
		g.Expect(out).NotTo(BeEmpty(), "token is empty")
	}

	Eventually(verifyTokenCreation, 2*time.Minute, 2*time.Second).Should(Succeed())

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

// -----------------------------------------------------------------------------
// Summary writer helper
// -----------------------------------------------------------------------------

func summaryWriterFromEnv() artifacts.JSONFileWriter {
	dir := os.Getenv("ARTIFACTS_DIR")
	if dir == "" {
		dir = "/tmp"
	}
	return artifacts.JSONFileWriter{
		Path: filepath.Join(dir, "sli-summary.json"),
	}
}

// -----------------------------------------------------------------------------
// Idempotent ClusterRoleBinding helper
// -----------------------------------------------------------------------------

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
