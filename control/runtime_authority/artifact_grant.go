// Copyright 2026 Mindclade. All rights reserved.
package runtime_authority

// ArtifactGrant bounds immutable input reads and output namespace writes.
type ArtifactGrant struct {
	ReadableDigests      []string
	WritableNamespaces   []string
	MaximumReadBytes     uint64
	MaximumWriteBytes    uint64
	AllowRangeReads      bool
	AllowMultipartWrites bool
}

func (g ArtifactGrant) Validate() error {
	if err := requireSortedUnique(g.ReadableDigests, "readable_digests"); err != nil {
		return err
	}
	if err := requireSortedUnique(g.WritableNamespaces, "writable_namespaces"); err != nil {
		return err
	}
	if g.MaximumReadBytes == 0 && len(g.ReadableDigests) > 0 {
		return invalid("artifact_read_budget_required", "artifact read budget is required", nil)
	}
	if g.MaximumWriteBytes == 0 && len(g.WritableNamespaces) > 0 {
		return invalid("artifact_write_budget_required", "artifact write budget is required", nil)
	}
	return nil
}
func (g ArtifactGrant) canonicalBytes() ([]byte, error) {
	if err := g.Validate(); err != nil {
		return nil, err
	}
	e := newCanonicalEncoder("artifact-grant")
	e.stringSet("readable_digests", g.ReadableDigests)
	e.stringSet("writable_namespaces", g.WritableNamespaces)
	e.u64("maximum_read_bytes", g.MaximumReadBytes)
	e.u64("maximum_write_bytes", g.MaximumWriteBytes)
	e.boolean("allow_range_reads", g.AllowRangeReads)
	e.boolean("allow_multipart_writes", g.AllowMultipartWrites)
	return e.bytes()
}
