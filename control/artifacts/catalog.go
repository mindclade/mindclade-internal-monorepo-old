// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package artifacts

import (
	"context"
	"strings"

	"go.mindclade.dev/libs/go/identifiers"
)

// MaximumLocationsPerArtifact bounds the replica set one digest may carry.
//
// The catalog is append-only by construction -- the digest/metadata binding is
// permanent and there is no Delete -- so an unbounded location set is a durable
// leak rather than a transient overload: nothing ever reclaims the rows, and a
// Locations read would grow without limit. A replicated artifact lives in a
// handful of providers, regions, and generations, so this is far above any
// legitimate fan-out while still being a number the reader can hold.
const MaximumLocationsPerArtifact = 32

type Ref struct {
	Digest        identifiers.Digest `json:"digest"`
	SizeBytes     uint64             `json:"size_bytes"`
	MediaType     string             `json:"media_type"`
	LogicalKind   string             `json:"logical_kind"`
	SchemaVersion uint32             `json:"schema_version"`
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

// SamePlacement reports whether two locations name the same durable placement.
// Region is deliberately excluded: it is descriptive metadata about where a
// generation happens to sit, so treating it as part of the key would let the
// same object be listed twice under two spellings of its region.
func (l Location) SamePlacement(o Location) bool {
	return l.Provider == o.Provider && l.URI == o.URI && l.Generation == o.Generation
}

// Catalog is the durable seam for artifact identity and placement.
//
// Two properties every implementation must preserve:
//
//   - The digest/metadata binding is permanent. Re-registering a digest with
//     different immutable metadata is ReasonIdentityConflict, never an
//     overwrite.
//   - A location may not exist without a matching identity. Writing one for an
//     absent or differing identity is ReasonLocationUnknownIdentity.
//
// Register exists because Put followed by N PutLocation calls has no commit
// boundary: a crash between them leaves a registered identity with no location.
// An implementation must make Register a single durable write. Put and
// PutLocation remain for callers that genuinely have only one of the two, and
// for the incremental case of adding a replica to an artifact that is already
// registered.
type Catalog interface {
	Put(context.Context, Ref) error
	Get(context.Context, identifiers.Digest) (Ref, error)
	PutLocation(context.Context, Location) error
	Locations(context.Context, identifiers.Digest) ([]Location, error)
	Register(context.Context, Ref, []Location) error
}

// validateRegistration runs every rejection the domain can make before any
// implementation touches durable state. Committing the identity first and
// validating the locations afterwards poisoned the digest permanently, so the
// corrected retry failed against state the caller never asked to keep.
func validateRegistration(r Ref, locations []Location) error {
	if err := r.Validate(); err != nil {
		return err
	}
	if len(locations) > MaximumLocationsPerArtifact {
		return locationBudgetExhausted()
	}
	for _, l := range locations {
		if err := l.Validate(); err != nil {
			return err
		}
		if !l.Artifact.EqualIdentity(r) {
			return invalid("artifact_location_identity_mismatch", "artifact location identity does not match registered artifact", nil)
		}
	}
	return nil
}
