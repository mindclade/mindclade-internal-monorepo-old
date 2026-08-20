// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package reference_databases

import (
	"go.mindclade.dev/control/artifacts"
	"go.mindclade.dev/libs/go/identifiers"
	"testing"
	"time"
)

func TestSealMakesSnapshotContentBound(t *testing.T) {
	id, _ := identifiers.NewID(identifiers.MustParseKind("refdb"))
	r := Release{ReleaseID: id.String(), Name: "uniref", Version: "2026-08", Kind: KindSequence, SourceCutoff: time.Now(), Shards: []artifacts.Ref{{Digest: identifiers.SHA256([]byte("s")), SizeBytes: 1, MediaType: "application/octet-stream", LogicalKind: "reference-shard", SchemaVersion: 1}}, IndexFormat: "mmseqs2", IndexTool: "mmseqs", IndexToolVersion: "1", SourceProvenanceDigest: identifiers.SHA256([]byte("p")), LicensePolicyDigest: identifiers.SHA256([]byte("l")), CompatibleSearchTools: []string{"mmseqs"}, Status: StatusQualified, Created: time.Now()}
	if err := r.Seal(); err != nil {
		t.Fatal(err)
	}
	old := r.SnapshotDigest
	r.IndexToolVersion = "2"
	if old.Equal(identifiers.SHA256([]byte(r.canonical(false)))) {
		t.Fatal("expected content change")
	}
}
