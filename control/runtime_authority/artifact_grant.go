// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package runtime_authority

import "strings"

// The Rust verifier (libs/rust/worker_protocol) rejects a grant that exceeds these, so a Go
// issuer without them mints signed tickets that every node refuses. The numbers are the
// verifier's (MAX_SET_ENTRIES and the namespace bound), not a second opinion: a looser
// issuer-side bound is the asymmetry, and the issuer is also the side that allocates first.
const (
	maximumGrantDigests    = 65536
	maximumGrantNamespaces = 4096
	maximumNamespaceBytes  = 1024
)

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
	if len(g.ReadableDigests) > maximumGrantDigests || len(g.WritableNamespaces) > maximumGrantNamespaces {
		return invalid("artifact_grant_entries_exceeded", "artifact grant exceeds entry bounds", nil)
	}
	if err := requireSortedUnique(g.ReadableDigests, "readable_digests"); err != nil {
		return err
	}
	if err := requireSortedUnique(g.WritableNamespaces, "writable_namespaces"); err != nil {
		return err
	}
	// A namespace that is absolute or contains `..` addresses storage outside the prefix the
	// grant names, so the write budget above would bound the volume of an escape rather than
	// prevent it. The verifier already refuses both; the issuer must not sign them.
	for _, namespace := range g.WritableNamespaces {
		if strings.TrimSpace(namespace) == "" || len(namespace) > maximumNamespaceBytes ||
			strings.HasPrefix(namespace, "/") || strings.Contains(namespace, "..") {
			return invalid("artifact_namespace_invalid", "artifact writable namespace is invalid", nil)
		}
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
