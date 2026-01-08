package e2e

import "time"

const testStartTimeAnnoKey = "test/start-time"

// setTestStartTimeAnno sets test/start-time annotation to current UTC time (RFC3339Nano).
// Important: annotation 을 넣기 위해서 metav1.Object(-> ann map[string]string) 를 사용하지 않는 것은 설령 e2e에 둔다 해도 “코어와 glue를 분리한다”는 전체 설계 철학상 glue 레이어 쪽 코드이기 때문이다.
func setTestStartTimeAnno(ann map[string]string) map[string]string {
	if ann == nil {
		ann = map[string]string{}
	}
	// TODO: 주입한 'start-time’과 ‘observe 시각’의 정의를 맞추고 싶을 때
	// TODO: 지금은 “주입 시각 = set 함수가 호출된 시각” 을, 호출자가 now를 잡고 여러 곳에 동일한 now를 쓰는 게 깔끔하다.
	// TODO: 이를 통해서, “리소스 생성/요청을 보낸 시각”을 더 정확히 기록할 수 있다. 다른 오퍼레이터에 적용할려면 중요한 이슈이다.
	// TODO: ann[testStartTimeAnnoKey] = now.UTC().Format(time.RFC3339Nano)
	// TODO: obj.Annotations = setTestStartTimeAnnoAt(obj.Annotations, time.Now()) 이렇게 사용.
	ann[testStartTimeAnnoKey] = time.Now().UTC().Format(time.RFC3339Nano)
	return ann
}
