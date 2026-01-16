package utils

import (
	"fmt"

	. "github.com/onsi/ginkgo/v2"

	instrumentv2 "github.com/yeongki/my-operator/pkg/slo/instrumentv2"
)

// GinkgoLogger adapts instrument.Logger to GinkgoWriter.
type GinkgoLogger struct{}

func (GinkgoLogger) Logf(format string, args ...any) {
	_, _ = fmt.Fprintf(GinkgoWriter, format+"\n", args...)
}

var _ instrumentv2.Logger = (*GinkgoLogger)(nil)
