// Copyright 2026 Mindclade. All rights reserved.
// Confidential and proprietary.

package outbox

import (
	"bytes"
	"encoding/json"
	"strings"
	"time"
	"unicode"

	"mindclade.internal/libs/go/faults"
	"mindclade.internal/libs/go/identifiers"
	"mindclade.internal/libs/go/requestmeta"
)

const (
	MaximumTopicLength       = 256
	MaximumPartitionKeyBytes = 1024
	MaximumContentTypeLength = 256
	MaximumHeaderCount       = 64
	MaximumHeaderKeyLength   = 128
	MaximumHeaderValueLength = 4096
	MaximumPayloadBytes      = 16 << 20
)

var MessageIDKind = identifiers.MustParseKind("outbox")

// Message is an immutable event awaiting publication.
type Message struct {
	id           identifiers.ID
	topic        string
	partitionKey string
	contentType  string
	payload      []byte
	headers      map[string]string
	request      requestmeta.Metadata
	createdAt    time.Time
	availableAt  time.Time
}

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
	value := Message{
		id:           id,
		topic:        strings.TrimSpace(topic),
		partitionKey: strings.TrimSpace(partitionKey),
		contentType:  strings.TrimSpace(contentType),
		payload:      append([]byte(nil), payload...),
		headers:      cloneHeaders(headers),
		request:      request,
		createdAt:    createdAt.Round(0).UTC(),
		availableAt:  availableAt.Round(0).UTC(),
	}
	if value.availableAt.IsZero() {
		value.availableAt = value.createdAt
	}
	if err := value.Validate(); err != nil {
		return Message{}, err
	}
	return value, nil
}

func (value Message) ID() identifiers.ID            { return value.id }
func (value Message) Topic() string                 { return value.topic }
func (value Message) PartitionKey() string          { return value.partitionKey }
func (value Message) ContentType() string           { return value.contentType }
func (value Message) Payload() []byte               { return append([]byte(nil), value.payload...) }
func (value Message) Headers() map[string]string    { return cloneHeaders(value.headers) }
func (value Message) Request() requestmeta.Metadata { return value.request }
func (value Message) CreatedAt() time.Time          { return value.createdAt }
func (value Message) AvailableAt() time.Time        { return value.availableAt }
func (value Message) PayloadSize() int              { return len(value.payload) }
func (value Message) Equal(other Message) bool {
	return value.id == other.id && value.topic == other.topic && value.partitionKey == other.partitionKey &&
		value.contentType == other.contentType && bytes.Equal(value.payload, other.payload) &&
		equalHeaders(value.headers, other.headers) && value.request == other.request &&
		value.createdAt.Equal(other.createdAt) && value.availableAt.Equal(other.availableAt)
}

func (value Message) Validate() error {
	if err := value.id.Validate(); err != nil || value.id.Kind() != MessageIDKind {
		return invalidMessage(err, "invalid_outbox_id")
	}
	if !validToken(value.topic, MaximumTopicLength) {
		return invalidMessage(ErrInvalidMessage, "invalid_outbox_topic")
	}
	if len(value.partitionKey) > MaximumPartitionKeyBytes || strings.TrimSpace(value.partitionKey) != value.partitionKey || containsControl(value.partitionKey) {
		return invalidMessage(ErrInvalidMessage, "invalid_outbox_partition_key")
	}
	if value.contentType == "" || len(value.contentType) > MaximumContentTypeLength || strings.TrimSpace(value.contentType) != value.contentType || containsControl(value.contentType) {
		return invalidMessage(ErrInvalidMessage, "invalid_outbox_content_type")
	}
	if len(value.payload) == 0 || len(value.payload) > MaximumPayloadBytes {
		return invalidMessage(ErrInvalidMessage, "invalid_outbox_payload")
	}
	if len(value.headers) > MaximumHeaderCount {
		return invalidMessage(ErrInvalidMessage, "too_many_outbox_headers")
	}
	for key, headerValue := range value.headers {
		if !validHeaderKey(key) || len(headerValue) > MaximumHeaderValueLength || containsControl(headerValue) {
			return invalidMessage(ErrInvalidMessage, "invalid_outbox_header")
		}
	}
	if err := value.request.Validate(); err != nil {
		return invalidMessage(err, "invalid_outbox_request_metadata")
	}
	if value.createdAt.IsZero() || value.availableAt.IsZero() || value.availableAt.Before(value.createdAt) {
		return invalidMessage(ErrInvalidMessage, "invalid_outbox_timestamps")
	}
	return nil
}

func (value Message) MarshalJSON() ([]byte, error) {
	if err := value.Validate(); err != nil {
		return nil, err
	}
	return json.Marshal(struct {
		ID           string               `json:"id"`
		Topic        string               `json:"topic"`
		PartitionKey string               `json:"partition_key,omitempty"`
		ContentType  string               `json:"content_type"`
		Payload      []byte               `json:"payload"`
		Headers      map[string]string    `json:"headers,omitempty"`
		Request      requestmeta.Metadata `json:"request,omitempty"`
		CreatedAt    time.Time            `json:"created_at"`
		AvailableAt  time.Time            `json:"available_at"`
	}{value.id.String(), value.topic, value.partitionKey, value.contentType, value.payload, cloneHeaders(value.headers), value.request, value.createdAt, value.availableAt})
}

func invalidMessage(cause error, reason string) error {
	if cause == nil {
		cause = ErrInvalidMessage
	}
	return faults.Wrap(cause, faults.CodeInvalidArgument, "invalid outbox message", faults.WithReason(reason), faults.WithOperation("outbox.Message.Validate"), faults.WithRetryPolicy(faults.NoRetry()))
}

func cloneHeaders(input map[string]string) map[string]string {
	if len(input) == 0 {
		return nil
	}
	output := make(map[string]string, len(input))
	for key, value := range input {
		output[key] = value
	}
	return output
}

func equalHeaders(left, right map[string]string) bool {
	if len(left) != len(right) {
		return false
	}
	for key, value := range left {
		if right[key] != value {
			return false
		}
	}
	return true
}

func validToken(value string, maximum int) bool {
	if value == "" || len(value) > maximum || strings.TrimSpace(value) != value {
		return false
	}
	for index, character := range value {
		if unicode.IsLower(character) || unicode.IsDigit(character) {
			continue
		}
		if index > 0 && (character == '.' || character == '-' || character == '_' || character == '/') {
			continue
		}
		return false
	}
	return true
}

func validHeaderKey(value string) bool {
	if value == "" || len(value) > MaximumHeaderKeyLength || strings.TrimSpace(value) != value {
		return false
	}
	for _, character := range value {
		if unicode.IsLower(character) || unicode.IsDigit(character) || character == '-' || character == '_' || character == '.' {
			continue
		}
		return false
	}
	return true
}

func containsControl(value string) bool {
	for _, character := range value {
		if unicode.IsControl(character) {
			return true
		}
	}
	return false
}
