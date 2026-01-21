package e2e

import (
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/yeongki/my-operator/test/utils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

const curlMetricsPodLabel = "app=curl-metrics"

// -----------------------------------------------------------------------------
// helpers_metrics.go
//
// 목표 (utils.Run 전제):
// - utils.Run()은 "성공 시 STDOUT만" 반환하므로 jsonpath 파싱 안정.
// - wrapper kubectl의 STDERR 배너가 STDOUT 파싱을 깨지 않도록 별도 runCmdStdout 제거.
// - 고정 podName 충돌 방지: 유니크 pod 이름 + label cleanup.
// - 테스트/계측 모두 best-effort로 다루기 쉽게, 함수 책임 분리.
//
// 스타일:
// - [OLD]/[NEW] 주석 유지
// - 최소 변경 + 가장 안전한 버전
// -----------------------------------------------------------------------------

// -----------------------------------------------------------------------------
// [OLD] 기존 고정 pod 방식 (참고용)
// -----------------------------------------------------------------------------

// const curlMetricsPodName = "curl-metrics"
//
// // runCurlMetricsOnce creates a short-lived curl pod (fixed name).
// // It does NOT wait; caller should wait for Succeeded before calling logs.
// func runCurlMetricsOnce(ns, token, metricsSvcName, serviceAccountName string) error {
// 	// Delete old pod if exists (ignore errors)
// 	_, _ = utils.Run(exec.Command(
// 		"kubectl", "delete", "pod", curlMetricsPodName,
// 		"-n", ns,
// 		"--ignore-not-found=true",
// 	))
//
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
//
// 	_, err := utils.Run(cmd)
// 	return err
// }
//
// func curlMetricsPhase(ns string) (string, error) {
// 	cmd := exec.Command(
// 		"kubectl", "get", "pods", curlMetricsPodName,
// 		"-o", "jsonpath={.status.phase}",
// 		"-n", ns,
// 	)
// 	return utils.Run(cmd)
// }
//
// func curlMetricsLogs(ns string) (string, error) {
// 	cmd := exec.Command("kubectl", "logs", curlMetricsPodName, "-n", ns)
// 	return utils.Run(cmd)
// }
//
// func cleanupCurlMetricsPod(ns string) {
// 	_, _ = utils.Run(exec.Command(
// 		"kubectl", "delete", "pod", curlMetricsPodName,
// 		"-n", ns,
// 		"--ignore-not-found=true",
// 	))
// }

// -----------------------------------------------------------------------------
// [NEW] 유니크 pod + label cleanup + utils.Run 일관 사용
//
// - 목적:
//   1) "curl-metrics" 고정 이름 충돌 제거 (재실행/동시성/flake 감소)
//   2) jsonpath 파싱 안정: utils.Run()이 성공 시 STDOUT만 반환하도록 이미 변경됨
//   3) best-effort cleanup: label 기반 정리 + per-pod 삭제
// -----------------------------------------------------------------------------

// runCurlMetricsOnce creates a short-lived curl pod.
// Returns the created pod name.
// It does NOT wait; caller should wait for Succeeded/Failed before calling logs.
func runCurlMetricsOnce(ns, token, metricsSvcName, serviceAccountName string) (string, error) {
	// best-effort cleanup of previous curl-metrics pods
	cleanupCurlMetricsPods(ns)

	podName := fmt.Sprintf("curl-metrics-%d", time.Now().UnixNano())
	metricsURL := fmt.Sprintf("https://%s.%s.svc:8443/metrics", metricsSvcName, ns)

	// NOTE:
	// - keep -k for self-signed cert in test env.
	// - keep output clean: avoid "-v" (headers/noise). (instrument parser should handle most cases anyway)
	// 	curlCmd := fmt.Sprintf(`set -euo pipefail;
	// echo "[curl-metrics] url=%s" >&2;
	// curl -ksS --fail-with-body -H "Authorization: Bearer %s" "%s";
	// `, metricsURL, token, metricsURL)

	curlCmd := fmt.Sprintf(`set -euo pipefail;
curl -ksS --fail-with-body -H "Authorization: Bearer %s" "%s";`, token, metricsURL)

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
	out, err := utils.Run(cmd) // 성공 시 STDOUT만 -> jsonpath 파싱 안정
	return strings.TrimSpace(out), err
}

func curlMetricsLogs(ns, podName string) (string, error) {
	cmd := exec.Command("kubectl", "logs", podName, "-n", ns)
	out, err := utils.Run(cmd) // logs는 파싱 민감하지 않음 (instrument가 expfmt로 파싱)
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

// deletePodNoWait delete pod best-effort (no wait) - used for per-pod cleanup.
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

// waitCurlMetricsDone waits until the curl pod reaches a terminal phase.
// It treats "Succeeded" or "Failed" as "done" (caller decides how to handle logs/errors).
func waitCurlMetricsDone(ns, podName string) {
	Eventually(func(g Gomega) {
		phase, err := curlMetricsPhase(ns, podName)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(phase == "Succeeded" || phase == "Failed").To(BeTrue(),
			"curl pod not done yet, phase=%s", phase)
	}, 5*time.Minute, 2*time.Second).Should(Succeed())
}

func waitControllerManagerReady(ns string) {
	Eventually(func(g Gomega) {
		out, err := utils.Run(exec.Command(
			"kubectl", "get", "pods",
			"-n", ns,
			"-l", "control-plane=controller-manager",
			"-o", "jsonpath={.items[0].status.containerStatuses[0].ready}",
		))
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(strings.TrimSpace(out)).To(Equal("true"))
	}, 5*time.Minute, 5*time.Second).Should(Succeed())
}

func waitServiceHasEndpoints(ns, svc string) {
	Eventually(func(g Gomega) {
		out, err := utils.Run(exec.Command(
			"kubectl", "get", "endpoints", svc,
			"-n", ns,
			"-o", "jsonpath={.subsets[0].addresses[0].ip}",
		))
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(strings.TrimSpace(out)).NotTo(BeEmpty())
	}, 5*time.Minute, 5*time.Second).Should(Succeed())
}
