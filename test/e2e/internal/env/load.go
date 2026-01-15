package env

import (
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/yeongki/my-operator/pkg/slo"
)

func LoadOptions() slo.Options {
	return slo.Options{
		Enabled: boolEnv("SLOLAB_ENABLED", false),

		ArtifactsDir: stringEnv("ARTIFACTS_DIR", "/tmp"),
		RunID:        stringEnv("CI_RUN_ID", ""),

		SkipCleanup:            boolEnv("E2E_SKIP_CLEANUP", false),
		SkipCertManagerInstall: boolEnv("CERT_MANAGER_INSTALL_SKIP", false),

		// 필요하면 이런 식으로 duration도 통일
		TokenRequestTimeout: durationEnv("TOKEN_REQUEST_TIMEOUT", 2*time.Minute),
	}
}

// --- helpers (규칙 통일: "1"/"true"/"yes"/"on" 모두 허용) ---

func stringEnv(key, def string) string {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return def
	}
	return v
}

func boolEnv(key string, def bool) bool {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return def
	}
	switch strings.ToLower(v) {
	case "1", "true", "t", "yes", "y", "on":
		return true
	case "0", "false", "f", "no", "n", "off":
		return false
	default:
		// 예기치 않은 값이면 def로 (또는 panic/로그 정책 선택 가능)
		return def
	}
}

func durationEnv(key string, def time.Duration) time.Duration {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return def
	}
	// 1) Go duration 포맷(예: "2m", "30s") 지원
	if d, err := time.ParseDuration(v); err == nil {
		return d
	}
	// 2) 숫자만 오면 초로 해석(예: "120" => 120s)
	if n, err := strconv.Atoi(v); err == nil {
		return time.Duration(n) * time.Second
	}
	return def
}
