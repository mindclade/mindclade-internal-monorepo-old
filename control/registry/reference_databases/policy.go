// Copyright 2026 Mindclade. All rights reserved.
package reference_databases

type PromotionPolicy struct {
	RequireQualified bool
	AllowedTools     map[string]bool
}

func (p PromotionPolicy) Allows(r Release) error {
	if p.RequireQualified && r.Status != StatusQualified && r.Status != StatusProduction {
		return invalid("reference_release_not_qualified", "reference release is not qualified", nil)
	}
	if len(p.AllowedTools) > 0 && !p.AllowedTools[r.IndexTool] {
		return invalid("reference_index_tool_not_allowed", "reference release index tool is not allowed", nil)
	}
	return nil
}
