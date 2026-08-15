// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package observability

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"testing"
	"time"

	"mindclade.internal/libs/go/auth"
	"mindclade.internal/libs/go/faults"
	"mindclade.internal/libs/go/requestmeta"
)

func TestAttributesDefensiveCopyRedactionAndMerge(t *testing.T) {
	source := faults.Fields{
		"service.component": "worker",
		"password":          "secret",
		"nested": faults.Fields{
			"access_token": "token",
			"count":        3,
		},
	}
	attributes, err := NewAttributes(source)
	if err != nil {
		t.Fatalf("NewAttributes() error = %v", err)
	}
	source["service.component"] = "mutated"
	fields := attributes.Fields()
	if got := fields["service.component"]; got != "worker" {
		t.Fatalf("service.component = %v, want worker", got)
	}
	if got := fields["password"]; got != faults.RedactedValue {
		t.Fatalf("password = %v, want redacted", got)
	}
	nested, ok := fields["nested"].(faults.Fields)
	if !ok {
		t.Fatalf("nested type = %T, want faults.Fields", fields["nested"])
	}
	if got := nested["access_token"]; got != faults.RedactedValue {
		t.Fatalf("nested access_token = %v, want redacted", got)
	}

	overlay := MustAttributes(faults.Fields{"service.component": "scheduler", "region": "us-central1"})
	merged := attributes.Merge(overlay).Fields()
	if got := merged["service.component"]; got != "scheduler" {
		t.Fatalf("merged service.component = %v, want scheduler", got)
	}
	if got := merged["region"]; got != "us-central1" {
		t.Fatalf("merged region = %v", got)
	}
}

func TestAttributesValidation(t *testing.T) {
	tooMany := make(faults.Fields, MaximumAttributeCount+1)
	for index := 0; index < MaximumAttributeCount+1; index++ {
		tooMany["field."+strings.Repeat("a", index%8)+string(rune('a'+index%26))] = index
	}
	// Generate unique valid keys even if the pattern above collides.
	tooMany = make(faults.Fields, MaximumAttributeCount+1)
	for index := 0; index < MaximumAttributeCount+1; index++ {
		tooMany["field."+time.Unix(int64(index+1), 0).UTC().Format("150405.000000000")] = index
	}
	if _, err := NewAttributes(tooMany); !errors.Is(err, ErrInvalidAttributes) {
		t.Fatalf("NewAttributes(too many) error = %v", err)
	}
	if _, err := NewAttributes(faults.Fields{"Invalid Key": 1}); !errors.Is(err, ErrInvalidAttributes) {
		t.Fatalf("NewAttributes(invalid key) error = %v", err)
	}
	if _, err := NewAttributes(faults.Fields{"metric.value": math.Inf(1)}); err == nil {
		t.Fatal("NewAttributes(non-finite) succeeded")
	}
}

func TestContextAndErrorAttributes(t *testing.T) {
	requestID, err := requestmeta.NewRequestIDAt(time.UnixMilli(1_700_000_000_000))
	if err != nil {
		t.Fatal(err)
	}
	ctx, err := requestmeta.WithMetadata(context.Background(), requestmeta.Metadata{
		RequestID: requestID,
		Operation: requestmeta.MustParseOperation("runs.Repository.Create"),
	})
	if err != nil {
		t.Fatal(err)
	}
	principal, err := auth.NewPrincipal(auth.PrincipalKindService, "scheduler", auth.WithIssuer("mindclade"))
	if err != nil {
		t.Fatal(err)
	}
	ctx, err = auth.WithPrincipal(ctx, principal)
	if err != nil {
		t.Fatal(err)
	}
	fields := ContextAttributes(ctx).Fields()
	if fields["request.id"] != requestID.String() {
		t.Fatalf("request.id = %v", fields["request.id"])
	}
	if fields["operation.name"] != "runs.Repository.Create" {
		t.Fatalf("operation.name = %v", fields["operation.name"])
	}
	if fields["principal.kind"] != "service" || fields["principal.key"] == "" {
		t.Fatalf("principal fields = %#v", fields)
	}
	if _, exists := fields["principal.subject"]; exists {
		t.Fatal("subject leaked into context attributes")
	}

	privateCause := errors.New("postgres password=secret")
	failure := faults.Wrap(privateCause, faults.CodeUnavailable, "repository unavailable",
		faults.WithReason("repository_unavailable"),
		faults.WithOperation("runs.Repository.Create"),
		faults.WithRetryPolicy(faults.BackoffRetry(3)),
	)
	errorFields := ErrorAttributes(failure).Fields()
	if errorFields["error.message"] != "repository unavailable" {
		t.Fatalf("error.message = %v", errorFields["error.message"])
	}
	for _, value := range errorFields {
		if strings.Contains(strings.ToLower(strings.TrimSpace(valueString(value))), "postgres") {
			t.Fatalf("private cause leaked: %#v", errorFields)
		}
	}
}

func valueString(value any) string {
	if text, ok := value.(string); ok {
		return text
	}
	return ""
}

func TestLabels(t *testing.T) {
	labels, err := NewLabels(map[string]string{"component": "scheduler", "result": "success"})
	if err != nil {
		t.Fatal(err)
	}
	copy := labels.Map()
	copy["component"] = "mutated"
	if labels.Map()["component"] != "scheduler" {
		t.Fatal("labels were mutated through returned map")
	}
	if _, err := NewLabels(map[string]string{"access_token": "value"}); !errors.Is(err, ErrInvalidLabels) {
		t.Fatalf("sensitive label error = %v", err)
	}
	if _, err := NewLabels(map[string]string{"request_id": "request_123"}); err != nil {
		// The package bounds label shape, while callers remain responsible for
		// choosing low-cardinality semantic labels. request_id is not treated as
		// a secret and therefore remains syntactically valid.
		t.Fatalf("request_id label unexpectedly rejected: %v", err)
	}
}

func TestAttributeAndLabelMergePreserveOverlayAtBounds(t *testing.T) {
	baseFields := make(faults.Fields, MaximumAttributeCount)
	for index := 0; index < MaximumAttributeCount; index++ {
		baseFields[fmt.Sprintf("base.%02d", index)] = index
	}
	base := MustAttributes(baseFields)
	overlay := MustAttributes(faults.Fields{"request.id": "request_value", "trace.id": "trace_value"})
	merged := base.Merge(overlay)
	if merged.Len() != MaximumAttributeCount {
		t.Fatalf("merged attribute count = %d", merged.Len())
	}
	fields := merged.Fields()
	if fields["request.id"] != "request_value" || fields["trace.id"] != "trace_value" {
		t.Fatalf("overlay attributes were dropped: %#v", fields)
	}

	baseLabels := make(map[string]string, MaximumLabelCount)
	for index := 0; index < MaximumLabelCount; index++ {
		baseLabels[fmt.Sprintf("base_%02d", index)] = "value"
	}
	mergedLabels := MustLabels(baseLabels).Merge(MustLabels(map[string]string{"result": "success"}))
	if mergedLabels.Len() != MaximumLabelCount || mergedLabels.Map()["result"] != "success" {
		t.Fatalf("merged labels = %#v", mergedLabels.Map())
	}
}
