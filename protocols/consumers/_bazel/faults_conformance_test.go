// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

// This file binds libs/go/faults to the protobuf bindings Bazel generates from
// protocols/proto/mindclade/common/v1/errors.proto, which is the declared wire
// authority for fault classification.
//
// Until this file existed, nothing in Go referenced the generated bindings at
// all: `protocols/gen/go` is a Bazel action output with no directory in the
// source tree, and the only Go file that imported it was
// generated_go_test.go, which asserts that the ten packages link and register
// descriptors — not that anything agrees with them. Every Go type that crosses
// the wire was therefore hand-written against a proto no build step compared it
// to, and the taxonomy drifted three ways before anyone noticed: the proto
// declared ten codes while Go declared seventeen and Rust sixteen, Go emitted
// "canceled"/"not_implemented" where Rust emitted "cancelled"/"unimplemented",
// and Rust's parser hard-errored on the spellings Go put on the wire.
//
// tests/integration/cross_language/test_error_codes.py already re-checks that
// reconciliation, but it does so with regular expressions over three source
// files. It reads the text of errors.proto, not the descriptor protoc built
// from it, so it cannot see a divergence that only exists after compilation and
// it goes stale the moment any of the three files is formatted differently.
// The assertions below read the compiled descriptor and call the Go package,
// so what they compare is what the two sides will actually do at runtime.
//
// Why this is a conformance binding rather than direct adoption of the
// generated types (see the pull request for the full argument):
//
//   - libs/go/faults is a Layer 0 foundation package. libs/go/LAYERS.md says
//     Layer 0 "uses only the Go standard library"; the generated bindings pull
//     in google.golang.org/protobuf, so faults cannot import them and remain
//     Layer 0.
//   - More decisively, no file inside the Go module can import them at all.
//     go_proto_library synthesizes go.mindclade.dev/protocols/gen/go/... as a
//     Bazel action output; nothing is checked in and go.mod resolves nothing
//     under that path, so `go build ./...` would fail on the first production
//     import. That is why this file lives in an underscore-prefixed Bazel
//     package: the Go module loader skips those directories, so only Bazel
//     compiles it.
//
// The binding is therefore the enforcement, and it is release-blocking:
// //protocols/consumers:generated_go_test is a qualification target of the
// protobuf-contracts release in ci/release/targets.yaml.
package consumers_test

import (
	"slices"
	"strings"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"

	"go.mindclade.dev/libs/go/faults"
	commonv1 "go.mindclade.dev/protocols/gen/go/mindclade/common/v1"
)

const (
	errorCodePrefix = "ERROR_CODE_"
	retryKindPrefix = "RETRY_KIND_"
)

// enumValue pairs a proto enum value's wire spelling with the tag it carries.
// The tag travels with the name because a set comparison alone would accept a
// renumbering, and renumbering silently reinterprets every message already in
// flight or already persisted.
type enumValue struct {
	wire string
	tag  protoreflect.EnumNumber
}

// protoWireValues reads an enum out of the descriptor protoc compiled, lowering
// each value name to the spelling the Go and Rust libraries put on the wire.
// The zero value is returned separately: proto3 requires a zero-valued
// UNSPECIFIED member, and neither language mirror represents it as a named code.
func protoWireValues(
	t *testing.T,
	descriptor protoreflect.EnumDescriptor,
	prefix string,
) (values []enumValue, unspecified string) {
	t.Helper()

	values = make([]enumValue, 0, descriptor.Values().Len())
	for index := range descriptor.Values().Len() {
		value := descriptor.Values().Get(index)
		name := string(value.Name())
		if !strings.HasPrefix(name, prefix) {
			t.Fatalf("%s value %q is missing the %q prefix", descriptor.FullName(), name, prefix)
		}
		wire := strings.ToLower(strings.TrimPrefix(name, prefix))
		if value.Number() == 0 {
			if unspecified != "" {
				t.Fatalf("%s declares two zero values", descriptor.FullName())
			}
			unspecified = wire
			continue
		}
		values = append(values, enumValue{wire: wire, tag: value.Number()})
	}

	if unspecified == "" {
		t.Fatalf("%s declares no zero value", descriptor.FullName())
	}
	slices.SortFunc(values, func(left, right enumValue) int { return int(left.tag - right.tag) })
	return values, unspecified
}

func errorCodeValues(t *testing.T) []enumValue {
	t.Helper()
	values, unspecified := protoWireValues(
		t, commonv1.ErrorCode(0).Descriptor(), errorCodePrefix)
	if unspecified != "unspecified" {
		t.Fatalf("ErrorCode zero value is %q, want %q", unspecified, "unspecified")
	}
	return values
}

func retryKindValues(t *testing.T) []enumValue {
	t.Helper()
	values, unspecified := protoWireValues(
		t, commonv1.RetryKind(0).Descriptor(), retryKindPrefix)
	if unspecified != "unspecified" {
		t.Fatalf("RetryKind zero value is %q, want %q", unspecified, "unspecified")
	}
	return values
}

// TestGoErrorCodesMatchTheGeneratedEnum is the assertion the taxonomy never had:
// the set libs/go/faults will accept, compared against the set the compiled
// descriptor declares, with no source text in between.
func TestGoErrorCodesMatchTheGeneratedEnum(t *testing.T) {
	generated := make(map[string]protoreflect.EnumNumber)
	for _, value := range errorCodeValues(t) {
		generated[value.wire] = value.tag
	}

	declared := make(map[string]struct{})
	for _, code := range faults.Codes() {
		if !code.Valid() {
			t.Fatalf("faults.Codes returned %q, which faults.Code.Valid rejects", code)
		}
		if _, duplicate := declared[string(code)]; duplicate {
			t.Fatalf("faults.Codes lists %q twice", code)
		}
		declared[string(code)] = struct{}{}
	}

	for wire := range generated {
		if _, ok := declared[wire]; !ok {
			t.Errorf(
				"mindclade.common.v1.ErrorCode declares %q and libs/go/faults does not; "+
					"a peer that sends it degrades to CodeUnknown and loses the sender's "+
					"retry choice",
				wire,
			)
		}
	}
	for wire := range declared {
		if _, ok := generated[wire]; !ok {
			t.Errorf(
				"libs/go/faults declares %q and mindclade.common.v1.ErrorCode does not; "+
					"Go can emit a code the wire authority cannot express",
				wire,
			)
		}
	}
}

// TestGoErrorCodeOrderFollowsTheGeneratedTagOrder holds faults.Codes to the
// order its own documentation promises. The promise is worth enforcing because
// it is what lets a reviewer read the const block and the proto side by side;
// once the two orders diverge, verifying the pair by eye stops working and the
// only remaining check is this file.
func TestGoErrorCodeOrderFollowsTheGeneratedTagOrder(t *testing.T) {
	generated := errorCodeValues(t)
	declared := faults.Codes()
	if len(generated) != len(declared) {
		t.Fatalf(
			"ErrorCode declares %d non-zero values and faults.Codes returns %d; "+
				"TestGoErrorCodesMatchTheGeneratedEnum names the difference",
			len(generated), len(declared),
		)
	}
	for index, value := range generated {
		if string(declared[index]) != value.wire {
			t.Errorf(
				"faults.Codes()[%d] = %q, want %q (ErrorCode tag %d)",
				index, declared[index], value.wire, value.tag,
			)
		}
	}
}

// TestGeneratedErrorCodesParseOnTheGoSide proves ingestion, not just naming: a
// peer that emits any value the descriptor declares must land on the matching
// Go code rather than on the catch-all.
func TestGeneratedErrorCodesParseOnTheGoSide(t *testing.T) {
	for _, value := range errorCodeValues(t) {
		parsed, err := faults.ParseCode(value.wire)
		if err != nil {
			t.Errorf("faults.ParseCode(%q) failed: %v", value.wire, err)
			continue
		}
		if string(parsed) != value.wire {
			t.Errorf("faults.ParseCode(%q) = %q, want %q", value.wire, parsed, value.wire)
		}
		if normalized := faults.NormalizeCode(parsed); normalized != parsed {
			t.Errorf(
				"faults.NormalizeCode(%q) = %q; the code the descriptor declares is "+
					"being rewritten on ingestion",
				parsed, normalized,
			)
		}
	}
}

// TestLegacyCodeSpellingsAreNotGeneratedValues keeps the compatibility aliases
// compatibility-only. If the proto ever declared "cancelled" alongside
// "canceled", one concept would have two canonical wire spellings and which one
// a receiver saw would depend on which peer wrote the message.
func TestLegacyCodeSpellingsAreNotGeneratedValues(t *testing.T) {
	generated := make(map[string]struct{})
	for _, value := range errorCodeValues(t) {
		generated[value.wire] = struct{}{}
	}

	aliases := faults.CodeAliases()
	if len(aliases) == 0 {
		t.Fatal("faults.CodeAliases is empty; the pre-reconciliation spellings must still parse")
	}
	for spelling, code := range aliases {
		if _, collides := generated[spelling]; collides {
			t.Errorf(
				"mindclade.common.v1.ErrorCode declares %q, which libs/go/faults treats as a "+
					"legacy alias for %q; the alias must be promoted or the proto value removed",
				spelling, code,
			)
		}
		if _, ok := generated[string(code)]; !ok {
			t.Errorf(
				"alias %q resolves to %q, which the generated enum does not declare",
				spelling, code,
			)
		}
	}
}

// TestGoRetryKindsMatchTheGeneratedEnum covers the other half of the contract.
// RetryKind exists because a delay alone cannot distinguish "the sender refuses
// a retry" from "the sender said nothing", so a missing kind is not a cosmetic
// gap: it collapses two different instructions into one.
func TestGoRetryKindsMatchTheGeneratedEnum(t *testing.T) {
	declared := faults.RetryKinds()
	if len(declared) == 0 || declared[0] != faults.RetryKindUnspecified {
		t.Fatalf(
			"faults.RetryKinds must start with RetryKindUnspecified to line up with "+
				"RETRY_KIND_UNSPECIFIED at tag 0; got %v",
			declared,
		)
	}
	// The empty string is this package's spelling of the proto's UNSPECIFIED. It
	// is the single deliberate difference between the two vocabularies, so it is
	// dropped here by name rather than by pattern.
	named := declared[1:]

	generated := retryKindValues(t)
	if len(generated) != len(named) {
		t.Fatalf(
			"RetryKind declares %d non-zero values and faults.RetryKinds names %d: %v vs %v",
			len(generated), len(named), generated, named,
		)
	}
	for index, value := range generated {
		if string(named[index]) != value.wire {
			t.Errorf(
				"faults.RetryKinds()[%d] = %q, want %q (RetryKind tag %d)",
				index+1, named[index], value.wire, value.tag,
			)
		}
	}
}

// codeForTag maps a generated enum member back to the Go code, the way a
// receiver decoding a real ErrorDetail has to.
func codeForTag(t *testing.T, code commonv1.ErrorCode) faults.Code {
	t.Helper()
	value := code.Descriptor().Values().ByNumber(code.Number())
	if value == nil {
		t.Fatalf("ErrorCode tag %d is not declared by its own descriptor", code.Number())
	}
	wire := strings.ToLower(strings.TrimPrefix(string(value.Name()), errorCodePrefix))
	parsed, err := faults.ParseCode(wire)
	if err != nil {
		t.Fatalf("faults.ParseCode(%q) failed: %v", wire, err)
	}
	return parsed
}

// TestErrorDetailBytesRoundTripThroughFaults is the end-to-end claim: for every
// code the wire authority declares, real serialized ErrorDetail bytes decode
// into a libs/go/faults value that still carries the sender's classification
// and the sender's retry instruction.
//
// The set-equality tests above would still pass if the two enums agreed on
// names while the Go constructors quietly rewrote a code on the way in. This
// one goes through proto.Marshal and faults.New, so it fails if they do.
func TestErrorDetailBytesRoundTripThroughFaults(t *testing.T) {
	const retryAfter = 1500 * time.Millisecond

	for _, value := range errorCodeValues(t) {
		detail := &commonv1.ErrorDetail{
			Code:             commonv1.ErrorCode(value.tag),
			Message:          "peer rejected the request",
			RequestId:        "req-0001",
			RetryKind:        commonv1.RetryKind_RETRY_KIND_AFTER,
			RetryAfterMillis: proto.Uint64(uint64(retryAfter.Milliseconds())),
		}

		encoded, err := proto.Marshal(detail)
		if err != nil {
			t.Fatalf("proto.Marshal(%q): %v", value.wire, err)
		}
		decoded := &commonv1.ErrorDetail{}
		if err := proto.Unmarshal(encoded, decoded); err != nil {
			t.Fatalf("proto.Unmarshal(%q): %v", value.wire, err)
		}

		code := codeForTag(t, decoded.GetCode())
		if string(code) != value.wire {
			t.Fatalf("decoded ErrorCode tag %d as %q, want %q", value.tag, code, value.wire)
		}

		policy := faults.DelayedRetry(
			time.Duration(decoded.GetRetryAfterMillis())*time.Millisecond, 0)
		fault := faults.New(code, decoded.GetMessage(), faults.WithRetryPolicy(policy))

		if got := faults.CodeOf(fault); got != code {
			t.Errorf("faults.CodeOf = %q for a fault built from code %q", got, code)
		}
		if got := faults.PublicMessageOf(fault); got != detail.GetMessage() {
			t.Errorf("faults.PublicMessageOf = %q, want %q", got, detail.GetMessage())
		}
		if got := faults.RetryPolicyOf(fault); got.Kind != faults.RetryKindAfter ||
			got.After != retryAfter {
			t.Errorf(
				"faults.RetryPolicyOf = %+v, want kind %q after %s",
				got, faults.RetryKindAfter, retryAfter,
			)
		}
	}
}

// TestRetryAfterIsCarriedOnlyByTheAfterKind pins the rule errors.proto states in
// prose — retry_after_millis "is set only for this kind" — to the Go
// normalization that has to implement it. A delay surviving on RETRY_KIND_NEVER
// would turn an explicit refusal into a scheduled retry.
func TestRetryAfterIsCarriedOnlyByTheAfterKind(t *testing.T) {
	const delay = 750 * time.Millisecond

	for _, value := range retryKindValues(t) {
		kind := faults.RetryKind(value.wire)
		policy := faults.RetryPolicy{Kind: kind, After: delay}.Normalized()

		if !slices.Contains(faults.RetryKinds(), kind) {
			t.Fatalf("RetryKind %q has no counterpart in faults.RetryKinds", value.wire)
		}
		if kind == faults.RetryKindAfter {
			if policy.Kind != kind || policy.After != delay {
				t.Errorf("normalizing %q dropped the delay: %+v", kind, policy)
			}
			continue
		}
		if policy.Kind != kind {
			t.Errorf("normalizing %q changed the kind to %q", kind, policy.Kind)
		}
		if policy.After != 0 {
			t.Errorf(
				"normalizing %q kept a %s delay; errors.proto sets retry_after_millis "+
					"only for RETRY_KIND_AFTER",
				kind, policy.After,
			)
		}
	}
}

// TestNeverAndUnspecifiedRetryStayDistinguishable guards the reason RetryKind
// was added to the proto at all. Both serialize with no delay, so if they ever
// compared equal on the Go side the wire could no longer carry the difference
// between a refusal and silence.
func TestNeverAndUnspecifiedRetryStayDistinguishable(t *testing.T) {
	never := faults.NoRetry().Normalized()
	silent := faults.RetryPolicy{}.Normalized()

	if never == silent {
		t.Fatal("NoRetry and the zero RetryPolicy are indistinguishable")
	}
	if !never.Specified() || silent.Specified() {
		t.Errorf(
			"RetryPolicy.Specified cannot separate refusal from silence: never=%v silent=%v",
			never.Specified(), silent.Specified(),
		)
	}
	if never.Retryable() || silent.Retryable() {
		t.Errorf(
			"neither refusal nor silence permits a retry: never=%v silent=%v",
			never.Retryable(), silent.Retryable(),
		)
	}
}
