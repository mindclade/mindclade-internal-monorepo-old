// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package ingestion

import (
	"go.mindclade.dev/libs/go/identifiers"
	"net/url"
	"strings"
)

type SourceKind string

const (
	SourcePDB         SourceKind = "pdb"
	SourceUniProt     SourceKind = "uniprot"
	SourceRNAcentral  SourceKind = "rnacentral"
	SourceObjectStore SourceKind = "object_store"
	SourceHTTP        SourceKind = "http"
)

func (k SourceKind) Valid() bool {
	switch k {
	case SourcePDB, SourceUniProt, SourceRNAcentral, SourceObjectStore, SourceHTTP:
		return true
	}
	return false
}

type SourceSpec struct {
	SourceID string
	Kind     SourceKind
	Locator  string
	Version  string
	License  string
}

func (s SourceSpec) Validate() error {
	id, err := identifiers.ParseID(s.SourceID)
	if err != nil || id.Kind().String() != "source" {
		return invalid("source_id_invalid", "source id must be canonical source identifier", err)
	}
	if !s.Kind.Valid() || s.Version == "" || len(s.Version) > 256 || len(s.License) > 256 {
		return invalid("source_invalid", "source kind/version/license is invalid", nil)
	}
	parsed, err := url.Parse(s.Locator)
	if err != nil || parsed.Scheme == "" || parsed.Fragment != "" || parsed.User != nil {
		return invalid("source_locator_invalid", "source locator must be an absolute credential-free URI", err)
	}
	if strings.TrimSpace(s.Locator) != s.Locator || len(s.Locator) > 2048 {
		return invalid("source_locator_invalid", "source locator is invalid", nil)
	}
	return nil
}
