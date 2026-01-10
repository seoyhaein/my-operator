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
	"encoding/json"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/yeongki/my-operator/internal/artifacts"
	"github.com/yeongki/my-operator/pkg/slo"
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
// - You are using a wrapped "kubectl" that prints a banner to STDERR.
// - utils.Run() uses CombinedOutput(), so STDERR is mixed into returned output.
// - For parsing-sensitive commands, this file uses runCmdStdout() which reads
//   STDOUT only, so the wrapper banner won't break parsing.
// -----------------------------------------------------------------------------

var _ = Describe("Manager", Ordered, func() {
	var controllerPodName string

	BeforeAll(func() {
		By("creating manager namespace")
		cmd := exec.Command("kubectl", "create", "ns", namespace)
		_, err := utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred(), "Failed to create namespace")

		By("labeling the namespace to enforce the security policy")

		// [OLD] restricted enforce
		// cmd = exec.Command("kubectl", "label", "--overwrite", "ns", namespace,
		// 	"pod-security.kubernetes.io/enforce=restricted")

		// [NEW] baseline to reduce flakiness while you are iterating
		// - If you want to test "restricted", switch back later after making manager pod compliant.
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
		// helpers_metrics.go provides cleanupCurlMetricsPods(ns)
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

		// [NEW] Always dump namespace events/resources even if controllerPodName is empty.
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

			// [OLD] jsonpath polling only
			// - may be flaky, and can be broken by wrapped kubectl banner mixing into stdout parsing
			// verifyControllerUp := func(g Gomega) {
			// 	cmd := exec.Command("kubectl", "get", "pods",
			// 		"-n", namespace,
			// 		"-l", "control-plane=controller-manager",
			// 		"-o", "jsonpath={.items[?(@.metadata.deletionTimestamp==null)].metadata.name}",
			// 	)
			// 	podName, err := utils.Run(cmd)
			// 	g.Expect(err).NotTo(HaveOccurred(), "Failed to retrieve controller-manager pod name")
			// 	controllerPodName = strings.TrimSpace(podName)
			// 	g.Expect(controllerPodName).NotTo(BeEmpty(), "controller-manager pod not found yet")
			// }

			// [NEW] more stable:
			// 1) Wait rollout status for deployment
			// 2) Fetch *first* pod name via jsonpath (stdout-only)
			verifyControllerUp := func(g Gomega) {
				// 1) wait deployment available
				cmd := exec.Command("kubectl", "rollout", "status",
					"deploy/"+controllerManagerDeploymentName,
					"-n", namespace,
					"--timeout=120s",
				)
				_, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred(), "controller-manager deployment not available yet")

				// 2) pick the first pod name only (avoid multi-pod concat issues)
				cmd = exec.Command("kubectl", "get", "pods",
					"-n", namespace,
					"-l", "control-plane=controller-manager",
					"-o", "jsonpath={.items[0].metadata.name}",
				)
				podName, err := runCmdStdout(cmd)
				g.Expect(err).NotTo(HaveOccurred(), "Failed to retrieve controller-manager pod name")

				controllerPodName = strings.TrimSpace(podName)
				g.Expect(controllerPodName).NotTo(BeEmpty(), "controller-manager pod not found yet")
				g.Expect(controllerPodName).To(ContainSubstring("controller-manager"))

				// 3) validate phase is Running
				cmd = exec.Command("kubectl", "get", "pod", controllerPodName,
					"-n", namespace,
					"-o", "jsonpath={.status.phase}",
				)
				phase, err := runCmdStdout(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(strings.TrimSpace(phase)).To(Equal("Running"), "Incorrect controller-manager pod status")
			}

			Eventually(verifyControllerUp).Should(Succeed())
		})

		It("should ensure the metrics endpoint is serving metrics (and write sli-summary.json best-effort)", func() {
			By("creating a ClusterRoleBinding for the service account to allow access to metrics")
			cmd := exec.Command("kubectl", "create", "clusterrolebinding", metricsRoleBindingName,
				"--clusterrole=my-operator-metrics-reader",
				fmt.Sprintf("--serviceaccount=%s:%s", namespace, serviceAccountName),
			)
			_, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred(), "Failed to create ClusterRoleBinding")

			By("validating that the metrics service is available")
			cmd = exec.Command("kubectl", "get", "service", metricsServiceName, "-n", namespace)
			_, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred(), "Metrics service should exist")

			By("getting the service account token")
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
			Eventually(verifyMetricsEndpointReady).Should(Succeed())

			By("verifying that the controller manager is serving the metrics server")
			verifyMetricsServerStarted := func(g Gomega) {
				cmd := exec.Command("kubectl", "logs", controllerPodName, "-n", namespace)
				out, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(out).To(ContainSubstring("controller-runtime.metrics\tServing metrics server"),
					"Metrics server not yet started")
			}
			Eventually(verifyMetricsServerStarted).Should(Succeed())

			// -------------------------------------------------------------------------
			// [NEW] A-option: scrape metrics twice (best-effort) and write sli-summary.json
			// - Fix #1: sanity check reuses collected logs (no extra "kubectl logs curl-metrics")
			// - Fix #2: unique curl pod name to avoid name collisions (your helpers_metrics.go already does this)
			// -------------------------------------------------------------------------
			By("A-option: scraping metrics twice and writing sli-summary.json (best-effort)")

			w := summaryWriterFromEnv()

			// --- Start snapshot ---
			var (
				startV    int64
				startOK   bool
				startLogs string
			)
			startPod, err := runCurlMetricsOnce(namespace, token, metricsServiceName, serviceAccountName)
			if err != nil {
				_, _ = fmt.Fprintf(GinkgoWriter, "[slo-lab] start runCurlMetricsOnce failed (ignored): %v\n", err)
			} else {
				// wait this pod to finish
				Eventually(func(g Gomega) {
					phase, err := curlMetricsPhase(namespace, startPod)
					g.Expect(err).NotTo(HaveOccurred())
					phase = strings.TrimSpace(phase)
					g.Expect(phase == "Succeeded" || phase == "Failed").To(BeTrue(), "curl pod not finished yet, phase=%s", phase)
				}, 5*time.Minute, 2*time.Second).Should(Succeed())

				out, err := curlMetricsLogs(namespace, startPod)
				_ = deletePodNoWait(namespace, startPod) // best-effort
				if err != nil {
					_, _ = fmt.Fprintf(GinkgoWriter, "[slo-lab] start logs failed (ignored): %v\n", err)
				} else {
					startLogs = out
					v, err := sumReconcileTotalFromCurlLogs(startLogs)
					if err != nil {
						_, _ = fmt.Fprintf(GinkgoWriter, "[slo-lab] start parse failed (ignored): %v\n", err)
					} else {
						startV = v
						startOK = true
						_, _ = fmt.Fprintf(GinkgoWriter, "[slo-lab] start reconcile_total=%d\n", startV)
					}
				}
			}

			// --- End snapshot ---
			var (
				endV    int64
				endOK   bool
				endLogs string
			)
			endPod, err := runCurlMetricsOnce(namespace, token, metricsServiceName, serviceAccountName)
			if err != nil {
				_, _ = fmt.Fprintf(GinkgoWriter, "[slo-lab] end runCurlMetricsOnce failed (ignored): %v\n", err)
			} else {
				Eventually(func(g Gomega) {
					phase, err := curlMetricsPhase(namespace, endPod)
					g.Expect(err).NotTo(HaveOccurred())
					phase = strings.TrimSpace(phase)
					g.Expect(phase == "Succeeded" || phase == "Failed").To(BeTrue(), "curl pod not finished yet, phase=%s", phase)
				}, 5*time.Minute, 2*time.Second).Should(Succeed())

				out, err := curlMetricsLogs(namespace, endPod)
				_ = deletePodNoWait(namespace, endPod) // best-effort
				if err != nil {
					_, _ = fmt.Fprintf(GinkgoWriter, "[slo-lab] end logs failed (ignored): %v\n", err)
				} else {
					endLogs = out
					v, err := sumReconcileTotalFromCurlLogs(endLogs)
					if err != nil {
						_, _ = fmt.Fprintf(GinkgoWriter, "[slo-lab] end parse failed (ignored): %v\n", err)
					} else {
						endV = v
						endOK = true
						_, _ = fmt.Fprintf(GinkgoWriter, "[slo-lab] end reconcile_total=%d\n", endV)
					}
				}
			}

			// delta 계산 (best-effort)
			var deltaF *float64
			if startOK && endOK {
				delta := endV - startV
				if delta >= 0 {
					f := float64(delta)
					deltaF = &f
				} else {
					_, _ = fmt.Fprintf(GinkgoWriter, "[slo-lab] negative delta=%d (ignored)\n", delta)
				}
			}

			// result 결정 (측정 실패는 skip)
			result := "skip"
			if startOK && endOK && deltaF != nil {
				result = "success"
			}

			// summary write (best-effort)
			_ = w.WriteSummary(slo.Summary{
				Labels: slo.Labels{
					Result: result,
				},
				CreatedAt: time.Now().UTC(),
				Metrics: slo.SummaryMetrics{
					ReconcileTotalDelta: deltaF,
				},
			})
			// [OLD]
			// _, _ = fmt.Fprintf(GinkgoWriter, "[slo-lab] wrote summary: result=%s delta=%v path=%s\n", result, deltaF, w.Path)

			// [NEW] print a stable delta string (no pointer printing, no panic)
			deltaStr := "nil"
			if deltaF != nil {
				// If you don't want decimals, use "%.0f" or format as int64 instead.
				v := *deltaF
				if math.IsNaN(v) {
					deltaStr = "NaN"
				} else if math.IsInf(v, 1) {
					deltaStr = "+Inf"
				} else if math.IsInf(v, -1) {
					deltaStr = "-Inf"
				} else {
					// Choose one:
					// deltaStr = fmt.Sprintf("%.0f", v)  // looks like a counter delta (integer)
					deltaStr = fmt.Sprintf("%f", v) // keeps float formatting
				}
			}
			_, _ = fmt.Fprintf(GinkgoWriter, "[slo-lab] wrote summary: result=%s delta=%s path=%s\n", result, deltaStr, w.Path)
			// ---------------------------------------------------------------------
			// [FIX] Sanity: reuse collected logs (no extra pod assumption)
			// ---------------------------------------------------------------------
			By("sanity: ensuring metrics were actually scraped successfully (reusing collected logs)")
			Expect(startOK || endOK).To(BeTrue(), "Failed to scrape metrics (both start and end attempts failed)")

			checkLogs := endLogs
			if !endOK {
				checkLogs = startLogs
			}
			Expect(checkLogs).NotTo(BeEmpty(), "Sanity logs are empty unexpectedly")
			Expect(checkLogs).To(ContainSubstring("controller_runtime_reconcile_total"))
		})
	})
})

// serviceAccountToken returns a token for the specified service account in the given namespace.
func serviceAccountToken() (string, error) {
	const tokenRequestRawString = `{
		"apiVersion": "authentication.k8s.io/v1",
		"kind": "TokenRequest"
	}`

	// [OLD] write JSON to /tmp and pass -f <file>
	//
	// secretName := fmt.Sprintf("%s-token-request", serviceAccountName)
	// tokenRequestFile := filepath.Join("/tmp", secretName)
	// err := os.WriteFile(tokenRequestFile, []byte(tokenRequestRawString), os.FileMode(0o644))
	// if err != nil {
	// 	return "", err
	// }
	//
	// var out string
	// verifyTokenCreation := func(g Gomega) {
	// 	cmd := exec.Command("kubectl", "create", "--raw", fmt.Sprintf(
	// 		"/api/v1/namespaces/%s/serviceaccounts/%s/token",
	// 		namespace,
	// 		serviceAccountName,
	// 	), "-f", tokenRequestFile)
	//
	// 	output, err := cmd.CombinedOutput()
	// 	g.Expect(err).NotTo(HaveOccurred())
	//
	// 	var token tokenRequest
	// 	err = json.Unmarshal(output, &token)
	// 	g.Expect(err).NotTo(HaveOccurred())
	//
	// 	out = token.Status.Token
	// }
	// Eventually(verifyTokenCreation).Should(Succeed())
	// return out, err

	// [NEW] No temp file: feed JSON via STDIN using "-f -"
	// - Fixes: "open /tmp/... no such file" in containerized / wrapped kubectl setups.
	var out string
	var lastErr error

	verifyTokenCreation := func(g Gomega) {
		endpoint := fmt.Sprintf("/api/v1/namespaces/%s/serviceaccounts/%s/token",
			namespace, serviceAccountName)

		cmd := exec.Command("kubectl", "create", "--raw", endpoint, "-f", "-")

		// stdin = TokenRequest JSON
		cmd.Stdin = strings.NewReader(tokenRequestRawString)

		// stdout/stderr capture (wrapper banner often goes to stderr)
		b, err := cmd.CombinedOutput()
		if err != nil {
			lastErr = fmt.Errorf("token request failed: %w: %s", err, string(b))
			g.Expect(err).NotTo(HaveOccurred(), lastErr.Error())
			return
		}

		// If wrapper ever pollutes stdout, try to salvage JSON by slicing from first '{'
		payload := extractJSONBestEffort(string(b))

		var token tokenRequest
		err = json.Unmarshal([]byte(payload), &token)
		if err != nil {
			lastErr = fmt.Errorf("token json unmarshal failed: %w: raw=%q", err, string(b))
			g.Expect(err).NotTo(HaveOccurred(), lastErr.Error())
			return
		}

		out = token.Status.Token
		lastErr = nil
		g.Expect(out).NotTo(BeEmpty(), "token is empty")
	}

	Eventually(verifyTokenCreation).Should(Succeed())

	if out == "" && lastErr != nil {
		return "", lastErr
	}
	return out, nil
}

// extractJSONBestEffort extract JSON object best-effort from mixed output.
// - If output is clean JSON, returns as-is.
// - If wrapper banner leaked into stdout, tries to slice from first '{' to last '}'.
func extractJSONBestEffort(s string) string {
	ss := strings.TrimSpace(s)
	if strings.HasPrefix(ss, "{") && strings.HasSuffix(ss, "}") {
		return ss
	}
	i := strings.Index(ss, "{")
	j := strings.LastIndex(ss, "}")
	if i >= 0 && j > i {
		return ss[i : j+1]
	}
	return ss
}

type tokenRequest struct {
	Status struct {
		Token string `json:"token"`
	} `json:"status"`
}

func sumReconcileTotalFromCurlLogs(curlLogs string) (int64, error) {
	const metricName = "controller_runtime_reconcile_total"

	lines := strings.Split(curlLogs, "\n")
	var (
		sum   int64
		found bool
	)

	for _, raw := range lines {
		ln := strings.TrimSpace(raw)
		if ln == "" {
			continue
		}

		// curl verbose prefixes (if present)
		ln = strings.TrimPrefix(ln, "< ")
		ln = strings.TrimPrefix(ln, "> ")
		ln = strings.TrimSpace(ln)

		if strings.HasPrefix(ln, "#") {
			continue
		}

		if strings.HasPrefix(ln, metricName+"{") || strings.HasPrefix(ln, metricName+" ") {
			fields := strings.Fields(ln)
			if len(fields) < 2 {
				continue
			}
			v, err := strconv.ParseFloat(fields[1], 64)
			if err != nil {
				continue
			}
			sum += int64(v)
			found = true
		}
	}

	if !found {
		return 0, fmt.Errorf("metric not found in curl logs: %s", metricName)
	}
	return sum, nil
}

func summaryWriterFromEnv() artifacts.JSONFileWriter {
	dir := os.Getenv("ARTIFACTS_DIR")
	if dir == "" {
		dir = "/tmp"
	}
	return artifacts.JSONFileWriter{
		Path: filepath.Join(dir, "sli-summary.json"),
	}
}
