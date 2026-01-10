package e2e

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/yeongki/my-operator/test/utils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

const curlMetricsPodLabel = "app=curl-metrics"

// -----------------------------------------------------------------------------
// [OLD] 기존 고정 pod 방식 (참고용)
// -----------------------------------------------------------------------------

// const curlMetricsPodName = "curl-metrics"

// // runCurlMetricsOnce creates a short-lived curl pod and returns its logs.
// // It does NOT wait; caller should wait for Succeeded before calling logs.
// func runCurlMetricsOnce(ns, token, metricsSvcName, serviceAccountName string) error {
// 	// Delete old pod if exists (ignore errors)
// 	_, _ = utils.Run(exec.Command(
// 		"kubectl", "delete", "pod", curlMetricsPodName,
// 		"-n", ns,
// 		"--ignore-not-found=true",
// 	))

// 	cmd := exec.Command(
// 		"kubectl", "run", curlMetricsPodName,
// 		"--restart=Never",
// 		"--namespace", ns,
// 		"--image=curlimages/curl:latest",
// 		"--overrides",
// 		fmt.Sprintf(`{
// 			"spec": {
// 				"containers": [{
// 					"name": "curl",
// 					"image": "curlimages/curl:latest",
// 					"command": ["/bin/sh", "-c"],
// 					"args": ["curl -v -k -H 'Authorization: Bearer %s' https://%s.%s.svc.cluster.local:8443/metrics"],
// 					"securityContext": {
// 						"allowPrivilegeEscalation": false,
// 						"capabilities": { "drop": ["ALL"] },
// 						"runAsNonRoot": true,
// 						"runAsUser": 1000,
// 						"seccompProfile": { "type": "RuntimeDefault" }
// 					}
// 				}],
// 				"serviceAccount": "%s"
// 			}
// 		}`, token, metricsSvcName, ns, serviceAccountName),
// 	)

// 	_, err := utils.Run(cmd)
// 	return err
// }

// func curlMetricsPhase(ns string) (string, error) {
// 	cmd := exec.Command(
// 		"kubectl", "get", "pods", curlMetricsPodName,
// 		"-o", "jsonpath={.status.phase}",
// 		"-n", ns,
// 	)
// 	return utils.Run(cmd)
// }

// func curlMetricsLogs(ns string) (string, error) {
// 	cmd := exec.Command("kubectl", "logs", curlMetricsPodName, "-n", ns)
// 	return utils.Run(cmd)
// }

// func cleanupCurlMetricsPod(ns string) {
// 	_, _ = utils.Run(exec.Command(
// 		"kubectl", "delete", "pod", curlMetricsPodName,
// 		"-n", ns,
// 		"--ignore-not-found=true",
// 	))
// }

// -----------------------------------------------------------------------------
// [NEW] 유니크 pod + label cleanup + phase/stdout-only
// - 목적:
//   1) "curl-metrics" 고정 이름 충돌 제거
//   2) wrapper kubectl의 STDERR 배너가 jsonpath 파싱을 깨지 않게 STDOUT만 읽기
// -----------------------------------------------------------------------------

// runCurlMetricsOnce creates a short-lived curl pod.
// Returns the created pod name.
// It does NOT wait; caller should wait for Succeeded/Failed before calling logs.
func runCurlMetricsOnce(ns, token, metricsSvcName, serviceAccountName string) (string, error) {
	// best-effort cleanup of previous curl-metrics pods
	cleanupCurlMetricsPods(ns)

	podName := fmt.Sprintf("curl-metrics-%d", time.Now().UnixNano())

	metricsURL := fmt.Sprintf("https://%s.%s.svc:8443/metrics", metricsSvcName, ns)

	// NOTE: keep -k for self-signed cert in test env.
	// NOTE: do NOT use "-v" here, it adds noise; your parser already strips "< " / "> " if present.
	curlCmd := fmt.Sprintf(
		`set -euo pipefail;
echo "[curl-metrics] url=%s";
curl -ksS -H "Authorization: Bearer %s" "%s";
`, metricsURL, token, metricsURL,
	)

	cmd := exec.Command(
		"kubectl", "run", podName,
		"--restart=Never",
		"--namespace", ns,
		"--image=curlimages/curl:latest",
		"--labels", curlMetricsPodLabel,
		"--overrides",
		fmt.Sprintf(`{
  "apiVersion":"v1",
  "kind":"Pod",
  "metadata":{
    "name":"%s",
    "namespace":"%s",
    "labels":{"app":"curl-metrics"}
  },
  "spec":{
    "serviceAccountName":"%s",
    "restartPolicy":"Never",
    "containers":[{
      "name":"curl",
      "image":"curlimages/curl:latest",
      "command":["/bin/sh","-c",%q],
      "securityContext":{
        "allowPrivilegeEscalation": false,
        "capabilities": { "drop": ["ALL"] },
        "runAsNonRoot": true,
        "runAsUser": 1000,
        "seccompProfile": { "type": "RuntimeDefault" }
      }
    }]
  }
}`, podName, ns, serviceAccountName, curlCmd),
	)

	_, err := utils.Run(cmd)
	return podName, err
}

func curlMetricsPhase(ns, podName string) (string, error) {
	cmd := exec.Command(
		"kubectl", "get", "pod", podName,
		"-n", ns,
		"-o", "jsonpath={.status.phase}",
	)
	out, err := runCmdStdout(cmd) // STDOUT only
	return strings.TrimSpace(out), err
}

func curlMetricsLogs(ns, podName string) (string, error) {
	cmd := exec.Command("kubectl", "logs", podName, "-n", ns)
	out, err := utils.Run(cmd) // logs are not parsing-sensitive
	return out, err
}

func cleanupCurlMetricsPod(ns, podName string) {
	_ = deletePodNoWait(ns, podName)
}

func cleanupCurlMetricsPods(ns string) {
	By("best-effort: cleaning up curl-metrics pods")
	cmd := exec.Command(
		"kubectl", "delete", "pod",
		"-n", ns,
		"-l", curlMetricsPodLabel,
		"--ignore-not-found=true",
		"--wait=false",
	)
	_, _ = utils.Run(cmd)
}

// deletePodNoWait delete pod best-effort (no wait) - used for per-pod cleanup
func deletePodNoWait(ns, podName string) error {
	cmd := exec.Command(
		"kubectl", "delete", "pod", podName,
		"-n", ns,
		"--ignore-not-found=true",
		"--wait=false",
	)
	_, err := utils.Run(cmd)
	return err
}

// runCmdStdout executes cmd and returns STDOUT only.
// This avoids wrapper kubectl banners written to STDERR from breaking jsonpath parsing.
//func runCmdStdout(cmd *exec.Cmd) (string, error) {
//	cmd.Env = append(os.Environ(), "GO111MODULE=on")
//
//	command := strings.Join(cmd.Args, " ")
//	_, _ = fmt.Fprintf(GinkgoWriter, "running: %q\n", command)
//
//	stdout, err := cmd.Output() // STDOUT only
//	if err != nil {
//		combined, _ := cmd.CombinedOutput()
//		return "", fmt.Errorf("%q failed with error %q: %w", command, string(combined), err)
//	}
//	return string(stdout), nil
//}

// runCmdStdout executes cmd and returns STDOUT only.
// This avoids wrapper kubectl banners written to STDERR from breaking jsonpath parsing.
// STDOUT-only capture to avoid wrapper STDERR banner mixing
func runCmdStdout(cmd *exec.Cmd) (string, error) {
	cmd.Env = append(os.Environ(), "GO111MODULE=on")

	command := strings.Join(cmd.Args, " ")
	_, _ = fmt.Fprintf(GinkgoWriter, "running: %q\n", command)

	// capture stdout only
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return "", err
	}
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		return "", err
	}

	if err := cmd.Start(); err != nil {
		return "", err
	}

	stdoutBytes, _ := io.ReadAll(stdoutPipe)
	stderrBytes, _ := io.ReadAll(stderrPipe)

	if err := cmd.Wait(); err != nil {
		combined := bytes.NewBuffer(nil)
		combined.Write(stdoutBytes)
		combined.Write(stderrBytes)
		return "", fmt.Errorf("%q failed with error %q: %w", command, combined.String(), err)
	}

	// IMPORTANT: return stdout only
	return string(stdoutBytes), nil
}

// Optional convenience helper: wait for curl pod completion
func waitCurlMetricsDone(ns, podName string) {
	Eventually(func(g Gomega) {
		phase, err := curlMetricsPhase(ns, podName)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(phase == "Succeeded" || phase == "Failed").To(BeTrue(), "curl pod not done yet, phase=%s", phase)
	}, 5*time.Minute, 2*time.Second).Should(Succeed())
}
