package slo

import (
	"fmt"

	"k8s.io/klog/v2" // depguard 에서 금지한 패키지
)

func TestVerify() {
	klog.Info("이거 걸려야 함")
	fmt.Println("테스트")
}
