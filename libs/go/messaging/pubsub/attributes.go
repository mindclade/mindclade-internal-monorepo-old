// Copyright 2026 Mindclade. All rights reserved.
// Confidential, proprietary, and trade-secret information.

package pubsub

import (
	"context"
	"strings"
	"time"

	"mindclade.internal/libs/go/faults"
	"mindclade.internal/libs/go/identifiers"
	"mindclade.internal/libs/go/messaging"
	"mindclade.internal/libs/go/requestmeta"
)

const (
	attributeMessageID   = "mindclade-message-id"
	attributeContentType = "mindclade-content-type"
	attributeCreatedAt   = "mindclade-created-at"
)

var reservedAttributes = map[string]struct{}{
	attributeMessageID:                      {},
	attributeContentType:                    {},
	attributeCreatedAt:                      {},
	requestmeta.PropagationKeyRequestID:     {},
	requestmeta.PropagationKeyCorrelationID: {},
	requestmeta.PropagationKeyCausationID:   {},
}

func encode(message messaging.Message) (PublishMessage, error) {
	if err := message.Validate(); err != nil {
		return PublishMessage{}, err
	}
	attributes := message.Attributes()
	for key := range reservedAttributes {
		if _, exists := attributes[key]; exists {
			return PublishMessage{}, faults.New(
				faults.CodeInvalidArgument,
				"message uses a reserved Pub/Sub attribute",
				faults.WithReason("reserved_pubsub_attribute"),
				faults.WithOperation("messaging.pubsub.Encode"),
				faults.WithField("attribute", key),
				faults.WithRetryPolicy(faults.NoRetry()),
			)
		}
	}
	if attributes == nil {
		attributes = make(map[string]string)
	}
	attributes[attributeMessageID] = message.ID().String()
	attributes[attributeContentType] = message.ContentType()
	attributes[attributeCreatedAt] = message.CreatedAt().Format(time.RFC3339Nano)
	request := message.Request()
	if !request.RequestID.IsZero() {
		attributes[requestmeta.PropagationKeyRequestID] = request.RequestID.String()
	}
	if !request.CorrelationID.IsZero() {
		attributes[requestmeta.PropagationKeyCorrelationID] = request.CorrelationID.String()
	}
	if !request.CausationID.IsZero() {
		attributes[requestmeta.PropagationKeyCausationID] = request.CausationID.String()
	}
	return PublishMessage{Data: message.Payload(), Attributes: attributes, OrderingKey: message.OrderingKey()}, nil
}

func decode(config Config, delivery ProviderDelivery) (messaging.Message, error) {
	if delivery == nil {
		return messaging.Message{}, dataLoss("nil_provider_delivery", nil)
	}
	attributes := clone(delivery.Attributes())
	identifier, err := identifiers.ParseIDKind(attributes[attributeMessageID], messaging.MessageIDKind)
	if err != nil {
		return messaging.Message{}, dataLoss("invalid_provider_message_id", err)
	}
	contentType := strings.TrimSpace(attributes[attributeContentType])
	createdAt, err := time.Parse(time.RFC3339Nano, attributes[attributeCreatedAt])
	if err != nil {
		return messaging.Message{}, dataLoss("invalid_provider_created_at", err)
	}
	carrier := requestmeta.MapCarrier(attributes)
	metadataContext, err := requestmeta.Extract(context.Background(), carrier)
	if err != nil {
		return messaging.Message{}, dataLoss("invalid_provider_request_metadata", err)
	}
	metadata, _ := requestmeta.FromContext(metadataContext)
	for key := range reservedAttributes {
		delete(attributes, key)
	}
	message, err := messaging.NewMessage(
		identifier,
		config.Topic,
		strings.TrimSpace(delivery.OrderingKey()),
		contentType,
		delivery.Data(),
		attributes,
		metadata,
		createdAt,
	)
	if err != nil {
		return messaging.Message{}, dataLoss("invalid_provider_message", err)
	}
	return message, nil
}

func clone(values map[string]string) map[string]string {
	if len(values) == 0 {
		return nil
	}
	result := make(map[string]string, len(values))
	for key, value := range values {
		result[key] = value
	}
	return result
}

func dataLoss(reason string, cause error) error {
	return faults.Wrap(
		cause,
		faults.CodeDataLoss,
		"Pub/Sub delivery failed validation",
		faults.WithReason(reason),
		faults.WithOperation("messaging.pubsub.Decode"),
		faults.WithRetryPolicy(faults.NoRetry()),
	)
}
