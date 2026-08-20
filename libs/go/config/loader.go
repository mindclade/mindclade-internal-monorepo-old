// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package config

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"time"

	mcclock "go.mindclade.dev/libs/go/clock"
	"go.mindclade.dev/libs/go/faults"
	"go.mindclade.dev/libs/go/identifiers"
)

type Loader struct {
	fields  map[string]Field
	sources []Source
	clock   mcclock.Clock
}

func New(fields []Field, sources ...Source) (*Loader, error) {
	definitions := make(map[string]Field, len(fields))
	for _, field := range fields {
		field = field.normalized()
		if err := field.ValidateField(); err != nil {
			return nil, err
		}
		if _, exists := definitions[field.Key]; exists {
			return nil, invalid(ErrInvalidField, "duplicate_config_field", "config.New", field.Key, "")
		}
		definitions[field.Key] = field
	}
	if len(definitions) == 0 {
		return nil, invalid(ErrInvalidField, "empty_config_schema", "config.New", "", "")
	}
	captured := make([]Source, 0, len(sources))
	for _, source := range sources {
		if source == nil || strings.TrimSpace(source.Name()) == "" {
			return nil, invalid(ErrSourceFailure, "invalid_config_source", "config.New", "", "")
		}
		captured = append(captured, source)
	}
	return &Loader{fields: definitions, sources: captured, clock: mcclock.RealClock{}}, nil
}
func (loader *Loader) WithClock(value mcclock.Clock) *Loader {
	if loader != nil && value != nil {
		loader.clock = value
	}
	return loader
}
func (loader *Loader) Load(ctx context.Context) (Snapshot, error) {
	if ctx == nil || loader == nil {
		return Snapshot{}, invalid(ErrSourceFailure, "invalid_config_load_request", "config.Loader.Load", "", "")
	}
	values := make(map[string]string, len(loader.fields))
	origins := make(map[string]Origin, len(loader.fields))
	keys := make([]string, 0, len(loader.fields))
	for key := range loader.fields {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		field := loader.fields[key]
		if field.Default != nil {
			values[key] = *field.Default
			origins[key] = Origin{Source: "default", Secret: field.Secret, Reloadable: field.Reloadable}
		}
	}
	for _, source := range loader.sources {
		if err := ctx.Err(); err != nil {
			return Snapshot{}, faults.Wrap(err, faults.CodeOf(err), "configuration loading canceled", faults.WithReason("config_load_canceled"), faults.WithOperation("config.Loader.Load"), faults.WithRetryPolicy(faults.NoRetry()))
		}
		loaded, err := source.Load(ctx)
		if err != nil {
			return Snapshot{}, faults.Wrap(errors.Join(ErrSourceFailure, err), faults.CodeUnavailable, "configuration source failed", faults.WithReason("config_source_failed"), faults.WithOperation("config.Loader.Load"), faults.WithField("source", source.Name()), faults.WithRetryPolicy(faults.NoRetry()))
		}
		sourceKeys := make([]string, 0, len(loaded))
		for key := range loaded {
			sourceKeys = append(sourceKeys, key)
		}
		sort.Strings(sourceKeys)
		for _, key := range sourceKeys {
			field, known := loader.fields[key]
			if !known {
				return Snapshot{}, invalid(ErrUnknownKey, "unknown_config_key", "config.Loader.Load", key, source.Name())
			}
			value := loaded[key]
			if len(value) > MaximumValueBytes {
				return Snapshot{}, invalid(ErrInvalidValue, "config_value_too_large", "config.Loader.Load", key, source.Name())
			}
			values[key] = value
			origins[key] = Origin{Source: source.Name(), Secret: field.Secret, Reloadable: field.Reloadable}
		}
	}
	for _, key := range keys {
		field := loader.fields[key]
		value, exists := values[key]
		if field.Required && (!exists || value == "") {
			return Snapshot{}, invalid(ErrRequiredMissing, "required_config_missing", "config.Loader.Load", key, "")
		}
		if exists && field.Validate != nil {
			if err := field.Validate(value); err != nil {
				return Snapshot{}, faults.Wrap(errors.Join(ErrInvalidValue, err), faults.CodeInvalidArgument, "invalid configuration value", faults.WithReason("invalid_config_value"), faults.WithOperation("config.Loader.Load"), faults.WithField("key", key), faults.WithField("source", origins[key].Source), faults.WithRetryPolicy(faults.NoRetry()))
			}
		}
	}
	digestInput := make([]struct {
		Key, Value, Source string
		Secret             bool
	}, 0, len(values))
	for _, key := range keys {
		value, ok := values[key]
		if !ok {
			continue
		}
		origin := origins[key]
		encoded := value
		if origin.Secret {
			encoded = identifiers.SHA256String(value).String()
		}
		digestInput = append(digestInput, struct {
			Key, Value, Source string
			Secret             bool
		}{key, encoded, origin.Source, origin.Secret})
	}
	encoded, err := json.Marshal(digestInput)
	if err != nil {
		return Snapshot{}, faults.Wrap(err, faults.CodeInternal, "configuration digest encoding failed", faults.WithReason("config_digest_failed"), faults.WithOperation("config.Loader.Load"))
	}
	now := time.Now().UTC()
	if loader.clock != nil {
		now = loader.clock.Now().Round(0).UTC()
	}
	return Snapshot{values: values, origins: origins, digest: identifiers.SHA256(encoded), loadedAt: now}, nil
}
func invalid(cause error, reason, operation, key, source string) error {
	fields := faults.Fields{}
	if key != "" {
		fields["key"] = key
	}
	if source != "" {
		fields["source"] = source
	}
	return faults.Wrap(cause, faults.CodeInvalidArgument, "invalid service configuration", faults.WithReason(reason), faults.WithOperation(operation), faults.WithFields(fields), faults.WithRetryPolicy(faults.NoRetry()))
}
