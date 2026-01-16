package instrumentv2

// Logger is the minimal logging contract for pkg/slo/instrument.
// Keep it tiny so core stays independent from klog/logr/controller-runtime/Ginkgo.
type Logger interface {
	Logf(format string, args ...any)
}

// NewLogf returns a safe log function.
// If l is nil, it returns a no-op func.
func NewLogf(l Logger) func(string, ...any) {
	if l == nil {
		return func(string, ...any) {}
	}
	return l.Logf
}
