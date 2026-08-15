// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package requestmeta

import (
	"errors"

	"mindclade.internal/libs/go/faults"
)

// Metadata is the transport-neutral request correlation envelope. All fields
// are optional, but non-zero fields must be canonical.
type Metadata struct {
	RequestID     RequestID     `json:"request_id,omitempty"`
	CorrelationID CorrelationID `json:"correlation_id,omitempty"`
	CausationID   CausationID   `json:"causation_id,omitempty"`
	Operation     Operation     `json:"operation,omitempty"`
}

// New constructs metadata for requestID and uses the request ID as the default
// end-to-end correlation ID.
func New(requestID RequestID) (Metadata, error) {
	if err := requestID.Validate(); err != nil {
		return Metadata{}, err
	}
	correlationID, err := CorrelationIDFromRequestID(requestID)
	if err != nil {
		return Metadata{}, err
	}
	return Metadata{RequestID: requestID, CorrelationID: correlationID}, nil
}

func (metadata Metadata) IsZero() bool {
	return metadata.RequestID.IsZero() &&
		metadata.CorrelationID.IsZero() &&
		metadata.CausationID.IsZero() &&
		metadata.Operation.IsZero()
}

func (metadata Metadata) Validate() error {
	var validationErrors []error
	if !metadata.RequestID.IsZero() {
		if err := metadata.RequestID.Validate(); err != nil {
			validationErrors = append(validationErrors, err)
		}
	}
	if !metadata.CorrelationID.IsZero() && !metadata.CorrelationID.Valid() {
		validationErrors = append(validationErrors, ErrInvalidCorrelation)
	}
	if !metadata.CausationID.IsZero() && !metadata.CausationID.Valid() {
		validationErrors = append(validationErrors, ErrInvalidCausation)
	}
	if !metadata.Operation.IsZero() && !metadata.Operation.Valid() {
		validationErrors = append(validationErrors, ErrInvalidOperation)
	}
	if len(validationErrors) == 0 {
		return nil
	}
	return invalidArgument(
		errors.Join(append([]error{ErrInvalidMetadata}, validationErrors...)...),
		"invalid request metadata",
		"invalid_request_metadata",
		metadata.Fields(),
	)
}

// Merge overlays non-zero fields from other onto metadata.
func (metadata Metadata) Merge(other Metadata) Metadata {
	if !other.RequestID.IsZero() {
		metadata.RequestID = other.RequestID
	}
	if !other.CorrelationID.IsZero() {
		metadata.CorrelationID = other.CorrelationID
	}
	if !other.CausationID.IsZero() {
		metadata.CausationID = other.CausationID
	}
	if !other.Operation.IsZero() {
		metadata.Operation = other.Operation
	}
	return metadata
}

// WithDefaultCorrelation returns metadata with CorrelationID populated from
// RequestID when no explicit correlation ID exists.
func (metadata Metadata) WithDefaultCorrelation() Metadata {
	if metadata.CorrelationID.IsZero() && !metadata.RequestID.IsZero() {
		correlationID, err := CorrelationIDFromRequestID(metadata.RequestID)
		if err == nil {
			metadata.CorrelationID = correlationID
		}
	}
	return metadata
}

// Fields returns bounded values suitable for structured logs and faults.
func (metadata Metadata) Fields() faults.Fields {
	fields := faults.Fields{}
	if !metadata.RequestID.IsZero() {
		fields[faults.FieldRequestID] = metadata.RequestID.String()
	}
	if !metadata.CorrelationID.IsZero() {
		fields["correlation_id"] = metadata.CorrelationID.String()
	}
	if !metadata.CausationID.IsZero() {
		fields["causation_id"] = metadata.CausationID.String()
	}
	if !metadata.Operation.IsZero() {
		fields[faults.FieldOperation] = metadata.Operation.String()
	}
	if len(fields) == 0 {
		return nil
	}
	return fields
}
