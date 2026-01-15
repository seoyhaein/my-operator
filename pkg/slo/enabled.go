package slo

// Enabled Default OFF. Only enabled when SLOLAB_ENABLED=1
// TODO: pkg/slo는 "core measurement library"로 유지해야 한다.
// 이 파일의 os.Getenv 기반 토글(런타임 구성 방식)은 향후 라이브러리 사용자에게
// "enabled는 env로 결정한다"는 정책을 강요하는 형태가 될 수 있다.
//
// 가능한 개선 방향(권장):
// - pkg/slo 바깥(예: internal/config, cmd/main)에서 env/flag/config를 읽고
// - enabled(bool)를 pkg/slo.NewRecorder(enabled, logger) 같은 생성자에 주입한다.
//
// 즉, pkg/slo는 "enabled를 결정하는 방법"을 알지 않고,
// "enabled 값에 따라 동작"만 하도록 경계를 유지한다., 삭제 고려 대상.
//func Enabled() bool {
//	return os.Getenv("SLOLAB_ENABLED") == "1"
//}
