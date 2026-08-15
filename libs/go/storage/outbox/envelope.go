// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package outbox

import (
	"time"

	coordination "go.mindclade.dev/libs/go/coordination/outbox"
	"go.mindclade.dev/libs/go/identifiers"
	"go.mindclade.dev/libs/go/requestmeta"
)

// Envelope is an immutable event awaiting transactional publication.
// It is an alias of the canonical coordination message type.
type Envelope = coordination.Message

// Message is retained for callers that use the coordination vocabulary.
type Message = coordination.Message

const (
	MaximumTopicLength       = coordination.MaximumTopicLength
	MaximumPartitionKeyBytes = coordination.MaximumPartitionKeyBytes
	MaximumContentTypeLength = coordination.MaximumContentTypeLength
	MaximumHeaderCount       = coordination.MaximumHeaderCount
	MaximumHeaderKeyLength   = coordination.MaximumHeaderKeyLength
	MaximumHeaderValueLength = coordination.MaximumHeaderValueLength
	MaximumPayloadBytes      = coordination.MaximumPayloadBytes
)

var EnvelopeIDKind = coordination.MessageIDKind
var MessageIDKind = coordination.MessageIDKind

// NewEnvelope constructs and validates an immutable outbox envelope.
func NewEnvelope(
	id identifiers.ID,
	topic string,
	partitionKey string,
	contentType string,
	payload []byte,
	headers map[string]string,
	request requestmeta.Metadata,
	createdAt time.Time,
	availableAt time.Time,
) (Envelope, error) {
	return coordination.NewMessage(id, topic, partitionKey, contentType, payload, headers, request, createdAt, availableAt)
}

// NewMessage is the compatibility spelling of NewEnvelope.
func NewMessage(
	id identifiers.ID,
	topic string,
	partitionKey string,
	contentType string,
	payload []byte,
	headers map[string]string,
	request requestmeta.Metadata,
	createdAt time.Time,
	availableAt time.Time,
) (Message, error) {
	return NewEnvelope(id, topic, partitionKey, contentType, payload, headers, request, createdAt, availableAt)
}
