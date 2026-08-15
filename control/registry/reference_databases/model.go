// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package reference_databases

import (
	"go.mindclade.dev/control/artifacts"
	"go.mindclade.dev/libs/go/identifiers"
	"sort"
	"strings"
	"time"
)

type Kind string

const (
	KindSequence    Kind = "sequence"
	KindTemplate    Kind = "template"
	KindChemistry   Kind = "chemistry"
	KindNucleicAcid Kind = "nucleic_acid"
	KindComposite   Kind = "composite"
)

func (k Kind) Valid() bool {
	switch k {
	case KindSequence, KindTemplate, KindChemistry, KindNucleicAcid, KindComposite:
		return true
	}
	return false
}

type Status string

const (
	StatusDraft      Status = "draft"
	StatusQualified  Status = "qualified"
	StatusProduction Status = "production"
	StatusRetired    Status = "retired"
)

type Release struct {
	ReleaseID              string
	Name                   string
	Version                string
	Kind                   Kind
	SnapshotDigest         identifiers.Digest
	SourceCutoff           time.Time
	Shards                 []artifacts.Ref
	IndexFormat            string
	IndexTool              string
	IndexToolVersion       string
	SourceProvenanceDigest identifiers.Digest
	LicensePolicyDigest    identifiers.Digest
	CompatibleSearchTools  []string
	Status                 Status
	Created                time.Time
}

func (r *Release) Seal() error {
	if r == nil {
		return invalid("reference_release_required", "reference database release is required", nil)
	}
	sort.Slice(r.Shards, func(i, j int) bool { return r.Shards[i].Digest.String() < r.Shards[j].Digest.String() })
	sort.Strings(r.CompatibleSearchTools)
	r.SnapshotDigest = identifiers.SHA256([]byte(r.canonical(false)))
	return r.Validate()
}
func (r Release) canonical(includeDigest bool) string {
	var b strings.Builder
	b.WriteString("refdb/v1\n")
	for _, v := range []string{r.ReleaseID, r.Name, r.Version, string(r.Kind), r.SourceCutoff.UTC().Format(time.RFC3339Nano), r.IndexFormat, r.IndexTool, r.IndexToolVersion, r.SourceProvenanceDigest.String(), r.LicensePolicyDigest.String(), string(r.Status), r.Created.UTC().Format(time.RFC3339Nano)} {
		b.WriteString(v)
		b.WriteByte('\n')
	}
	if includeDigest {
		b.WriteString(r.SnapshotDigest.String())
		b.WriteByte('\n')
	}
	for _, s := range r.Shards {
		b.WriteString(s.Digest.String())
		b.WriteByte('|')
		b.WriteString(s.MediaType)
		b.WriteByte('|')
		b.WriteString(s.LogicalKind)
		b.WriteByte('\n')
	}
	for _, t := range r.CompatibleSearchTools {
		b.WriteString(t)
		b.WriteByte('\n')
	}
	return b.String()
}
