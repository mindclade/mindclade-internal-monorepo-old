// Copyright 2026 Mindclade. All rights reserved.
// Confidential, proprietary, and trade-secret information.

package messaging

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
	MaximumTopicLength         = 256
	MaximumOrderingKeyBytes    = 1024
	MaximumContentTypeLength   = 256
	MaximumAttributeCount      = 64
	MaximumAttributeKeyBytes   = 128
	MaximumAttributeValueBytes = 4096
	MaximumPayloadBytes        = 16 << 20
)

var MessageIDKind = identifiers.MustParseKind("message")

// Message is an immutable broker-neutral message. The payload is opaque to this
// package and its schema is owned by protocols/ or the publishing domain.
type Message struct {
	id          identifiers.ID
	topic       string
	orderingKey string
	contentType string
	payload     []byte
	attributes  map[string]string
	request     requestmeta.Metadata
	createdAt   time.Time
}

func NewMessage(
	id identifiers.ID,
	topic string,
	orderingKey string,
	contentType string,
	payload []byte,
	attributes map[string]string,
	request requestmeta.Metadata,
	createdAt time.Time,
) (Message, error) {
	value := Message{
		id:          id,
		topic:       strings.TrimSpace(topic),
		orderingKey: strings.TrimSpace(orderingKey),
		contentType: strings.TrimSpace(contentType),
		payload:     append([]byte(nil), payload...),
		attributes:  cloneAttributes(attributes),
		request:     request,
		createdAt:   createdAt.Round(0).UTC(),
	}
	if err := value.Validate(); err != nil {
		return Message{}, err
	}
	return value, nil
}

func (value Message) ID() identifiers.ID            { return value.id }
func (value Message) Topic() string                 { return value.topic }
func (value Message) OrderingKey() string           { return value.orderingKey }
func (value Message) ContentType() string           { return value.contentType }
func (value Message) Payload() []byte               { return append([]byte(nil), value.payload...) }
func (value Message) Attributes() map[string]string { return cloneAttributes(value.attributes) }
func (value Message) Request() requestmeta.Metadata { return value.request }
func (value Message) CreatedAt() time.Time          { return value.createdAt }
func (value Message) PayloadSize() int              { return len(value.payload) }

func (value Message) Equal(other Message) bool {
	return value.id == other.id && value.topic == other.topic && value.orderingKey == other.orderingKey &&
		value.contentType == other.contentType && bytes.Equal(value.payload, other.payload) &&
		equalAttributes(value.attributes, other.attributes) && value.request == other.request &&
		value.createdAt.Equal(other.createdAt)
}

func (value Message) Validate() error {
	if err := value.id.Validate(); err != nil || value.id.Kind() != MessageIDKind {
		return invalid(err, "invalid_message_id", "messaging.Message.Validate", nil)
	}
	if !validToken(value.topic, MaximumTopicLength) {
		return invalid(ErrInvalidMessage, "invalid_message_topic", "messaging.Message.Validate", faults.Fields{"topic": value.topic})
	}
	if len(value.orderingKey) > MaximumOrderingKeyBytes || strings.TrimSpace(value.orderingKey) != value.orderingKey || containsControl(value.orderingKey) {
		return invalid(ErrInvalidMessage, "invalid_message_ordering_key", "messaging.Message.Validate", nil)
	}
	if value.contentType == "" || len(value.contentType) > MaximumContentTypeLength || strings.TrimSpace(value.contentType) != value.contentType || containsControl(value.contentType) {
		return invalid(ErrInvalidMessage, "invalid_message_content_type", "messaging.Message.Validate", nil)
	}
	if len(value.payload) == 0 || len(value.payload) > MaximumPayloadBytes {
		return invalid(ErrInvalidMessage, "invalid_message_payload", "messaging.Message.Validate", faults.Fields{"payload_bytes": len(value.payload)})
	}
	if len(value.attributes) > MaximumAttributeCount {
		return invalid(ErrInvalidMessage, "too_many_message_attributes", "messaging.Message.Validate", faults.Fields{"attribute_count": len(value.attributes)})
	}
	for key, attributeValue := range value.attributes {
		if !validAttributeKey(key) || len(attributeValue) > MaximumAttributeValueBytes || containsControl(attributeValue) {
			return invalid(ErrInvalidMessage, "invalid_message_attribute", "messaging.Message.Validate", faults.Fields{"attribute_key": key})
		}
	}
	if err := value.request.Validate(); err != nil {
		return invalid(err, "invalid_message_request_metadata", "messaging.Message.Validate", nil)
	}
	if value.createdAt.IsZero() {
		return invalid(ErrInvalidMessage, "invalid_message_created_at", "messaging.Message.Validate", nil)
	}
	return nil
}

func (value Message) MarshalJSON() ([]byte, error) {
	if err := value.Validate(); err != nil {
		return nil, err
	}
	return json.Marshal(struct {
		ID          string               `json:"id"`
		Topic       string               `json:"topic"`
		OrderingKey string               `json:"ordering_key,omitempty"`
		ContentType string               `json:"content_type"`
		Payload     []byte               `json:"payload"`
		Attributes  map[string]string    `json:"attributes,omitempty"`
		Request     requestmeta.Metadata `json:"request,omitempty"`
		CreatedAt   time.Time            `json:"created_at"`
	}{value.id.String(), value.topic, value.orderingKey, value.contentType, value.payload, cloneAttributes(value.attributes), value.request, value.createdAt})
}

func cloneAttributes(input map[string]string) map[string]string {
	if len(input) == 0 {
		return nil
	}
	output := make(map[string]string, len(input))
	for key, value := range input {
		output[key] = value
	}
	return output
}

func equalAttributes(left, right map[string]string) bool {
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

func validAttributeKey(value string) bool {
	if value == "" || len(value) > MaximumAttributeKeyBytes || strings.TrimSpace(value) != value {
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
