// Copyright 2026 Mindclade. All rights reserved.
package artifacts

type AccessGrant struct {
	TenantID           string
	ReadableDigests    []string
	WritableNamespaces []string
	MaximumReadBytes   uint64
	MaximumWriteBytes  uint64
}

func (g AccessGrant) Validate() error {
	if g.TenantID == "" {
		return invalid("artifact_grant_tenant_required", "artifact grant tenant is required", nil)
	}
	if len(g.ReadableDigests) > 0 && g.MaximumReadBytes == 0 {
		return invalid("artifact_read_budget_required", "artifact read grant requires byte budget", nil)
	}
	if len(g.WritableNamespaces) > 0 && g.MaximumWriteBytes == 0 {
		return invalid("artifact_write_budget_required", "artifact write grant requires byte budget", nil)
	}
	return nil
}
