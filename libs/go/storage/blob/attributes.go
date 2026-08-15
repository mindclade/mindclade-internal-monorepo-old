// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package blob

import (
	"strings"
	"time"

	"mindclade.internal/libs/go/identifiers"
)

const MaximumContentTypeLength = 256

type Attributes struct {
	Key         Key                `json:"key"`
	Size        int64              `json:"size"`
	Digest      identifiers.Digest `json:"digest,omitempty"`
	ContentType string             `json:"content_type,omitempty"`
	ETag        string             `json:"etag,omitempty"`
	Generation  int64              `json:"generation"`
	CreatedAt   time.Time          `json:"created_at,omitempty"`
	UpdatedAt   time.Time          `json:"updated_at,omitempty"`
	Metadata    Metadata           `json:"metadata,omitempty"`
}

func (attributes Attributes) Validate() error {
	switch {
	case attributes.Key.Validate() != nil:
		return invalidArgument(nil, ErrInvalidObject, "invalid blob attributes", "invalid_blob_attribute_key", "blob.Attributes.Validate", nil)
	case attributes.Size < 0:
		return invalidArgument(nil, ErrInvalidObject, "invalid blob attributes", "invalid_blob_size", "blob.Attributes.Validate", nil)
	case attributes.Generation <= 0:
		return invalidArgument(nil, ErrInvalidObject, "invalid blob attributes", "invalid_blob_generation", "blob.Attributes.Validate", nil)
	case len(attributes.ContentType) > MaximumContentTypeLength || strings.TrimSpace(attributes.ContentType) != attributes.ContentType:
		return invalidArgument(nil, ErrInvalidObject, "invalid blob attributes", "invalid_blob_content_type", "blob.Attributes.Validate", nil)
	case attributes.ETag != "" && (len(attributes.ETag) > 512 || strings.TrimSpace(attributes.ETag) != attributes.ETag):
		return invalidArgument(nil, ErrInvalidObject, "invalid blob attributes", "invalid_blob_etag", "blob.Attributes.Validate", nil)
	case attributes.Metadata.Validate() != nil:
		return invalidArgument(nil, ErrInvalidObject, "invalid blob attributes", "invalid_blob_metadata", "blob.Attributes.Validate", nil)
	default:
		return nil
	}
}

func (attributes Attributes) Clone() Attributes {
	attributes.Metadata = attributes.Metadata.Clone()
	return attributes
}
