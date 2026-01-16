package instrumentv2

// CommonMetricDefs provides a minimal default set (v2 baseline).
func CommonMetricDefs() []MetricDef {
	return []MetricDef{
		{Name: "controller_runtime_reconcile_total", Scope: ScopeGlobal},
	}
}
