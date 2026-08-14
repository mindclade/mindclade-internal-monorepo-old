// Copyright 2026 Mindclade. All rights reserved.
package resourceversion

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"mindclade.internal/libs/go/faults"
	"mindclade.internal/libs/go/identifiers"
)

// Version is the canonical cross-language optimistic-concurrency token. It
// combines a monotonic generation with the digest of the durable representation
// observed at that generation. Zero is absence.
type Version struct {
	generation uint64
	digest     identifiers.Digest
}

func New(generation uint64, digest identifiers.Digest) (Version, error) {
	v := Version{generation: generation, digest: digest}
	if err := v.Validate(); err != nil {
		return Version{}, err
	}
	return v, nil
}
func Parse(value string) (Version, error) {
	if value == "" {
		return Version{}, invalid("empty_resource_version", value)
	}
	parts := strings.SplitN(value, ":", 3)
	if len(parts) != 3 || parts[0] != "rv1" {
		return Version{}, invalid("invalid_resource_version_schema", value)
	}
	generation, err := strconv.ParseUint(parts[1], 10, 64)
	if err != nil || generation == 0 || strings.HasPrefix(parts[1], "0") {
		return Version{}, invalid("invalid_resource_version_generation", value)
	}
	digest, err := identifiers.ParseDigest(parts[2])
	if err != nil {
		return Version{}, faults.Wrap(err, faults.CodeInvalidArgument, "invalid resource version digest", faults.WithReason("invalid_resource_version_digest"), faults.WithOperation("resourceversion.Parse"), faults.WithRetryPolicy(faults.NoRetry()))
	}
	return New(generation, digest)
}
func (v Version) Validate() error {
	if v.generation == 0 || !v.digest.Valid() {
		return invalid("invalid_resource_version", "generation and content digest are required")
	}
	return nil
}
func (v Version) IsZero() bool               { return v.generation == 0 && !v.digest.Valid() }
func (v Version) Generation() uint64         { return v.generation }
func (v Version) Digest() identifiers.Digest { return v.digest }
func (v Version) String() string {
	if v.IsZero() {
		return ""
	}
	return fmt.Sprintf("rv1:%d:%s", v.generation, v.digest.String())
}
func (v Version) Next(digest identifiers.Digest) (Version, error) {
	if err := v.Validate(); err != nil {
		return Version{}, err
	}
	if v.generation == ^uint64(0) {
		return Version{}, faults.Wrap(ErrInvalidVersion, faults.CodeOutOfRange, "resource version exhausted", faults.WithReason("resource_version_overflow"), faults.WithOperation("resourceversion.Version.Next"), faults.WithRetryPolicy(faults.NoRetry()))
	}
	return New(v.generation+1, digest)
}
func (v Version) ETag() string {
	if v.IsZero() {
		return ""
	}
	return `"` + v.String() + `"`
}
func ParseETag(value string) (Version, error) {
	if len(value) < 2 || value[0] != '"' || value[len(value)-1] != '"' {
		return Version{}, invalid("invalid_resource_version_etag", value)
	}
	return Parse(value[1 : len(value)-1])
}
func (v Version) MarshalText() ([]byte, error) {
	if v.IsZero() {
		return nil, nil
	}
	if err := v.Validate(); err != nil {
		return nil, err
	}
	return []byte(v.String()), nil
}
func (v *Version) UnmarshalText(value []byte) error {
	if v == nil {
		return invalid("nil_resource_version_destination", string(value))
	}
	if len(value) == 0 {
		*v = Version{}
		return nil
	}
	parsed, err := Parse(string(value))
	if err != nil {
		return err
	}
	*v = parsed
	return nil
}
func (v Version) MarshalJSON() ([]byte, error) {
	if v.IsZero() {
		return []byte("null"), nil
	}
	return json.Marshal(v.String())
}
func (v *Version) UnmarshalJSON(value []byte) error {
	if string(value) == "null" {
		*v = Version{}
		return nil
	}
	var text string
	if err := json.Unmarshal(value, &text); err != nil {
		return invalid("invalid_resource_version_json", string(value))
	}
	return v.UnmarshalText([]byte(text))
}
func (v Version) Value() (driver.Value, error) {
	if v.IsZero() {
		return nil, nil
	}
	if err := v.Validate(); err != nil {
		return nil, err
	}
	return v.String(), nil
}
func (v *Version) Scan(value any) error {
	if v == nil {
		return invalid("nil_resource_version_destination", "")
	}
	switch typed := value.(type) {
	case nil:
		*v = Version{}
		return nil
	case []byte:
		return v.UnmarshalText(typed)
	case string:
		return v.UnmarshalText([]byte(typed))
	default:
		return invalid("invalid_resource_version_sql_type", fmt.Sprintf("%T", value))
	}
}
func invalid(reason, value string) error {
	return faults.Wrap(ErrInvalidVersion, faults.CodeInvalidArgument, "invalid resource version", faults.WithReason(reason), faults.WithOperation("resourceversion"), faults.WithField("value", value), faults.WithRetryPolicy(faults.NoRetry()))
}
