// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package artifacts

import (
	"context"
	"mindclade.internal/libs/go/identifiers"
	"strings"
)

type Ref struct {
	Digest        identifiers.Digest
	SizeBytes     uint64
	MediaType     string
	LogicalKind   string
	SchemaVersion uint32
}

func (r Ref) Validate() error {
	if !r.Digest.Valid() {
		return invalid("artifact_digest_required", "artifact digest is required", nil)
	}
	if r.MediaType == "" || !strings.Contains(r.MediaType, "/") {
		return invalid("artifact_media_type_invalid", "artifact media type must be explicit", nil)
	}
	if r.LogicalKind == "" || r.SchemaVersion == 0 {
		return invalid("artifact_contract_required", "artifact logical kind and schema version are required", nil)
	}
	return nil
}
func (r Ref) EqualIdentity(o Ref) bool {
	return r.Digest.Equal(o.Digest) && r.SizeBytes == o.SizeBytes && r.MediaType == o.MediaType && r.LogicalKind == o.LogicalKind && r.SchemaVersion == o.SchemaVersion
}

type Location struct {
	Artifact   Ref
	Provider   string
	URI        string
	Generation string
	Region     string
}

func (l Location) Validate() error {
	if err := l.Artifact.Validate(); err != nil {
		return err
	}
	if l.Provider == "" || l.URI == "" || l.Generation == "" {
		return invalid("artifact_location_invalid", "artifact location requires provider, uri, and generation", nil)
	}
	return nil
}

type Catalog interface {
	Put(context.Context, Ref) error
	Get(context.Context, identifiers.Digest) (Ref, error)
	PutLocation(context.Context, Location) error
	Locations(context.Context, identifiers.Digest) ([]Location, error)
}
