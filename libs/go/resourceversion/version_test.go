// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package resourceversion

import (
	"encoding/json"
	"go.mindclade.dev/libs/go/identifiers"
	"testing"
)

func TestVersionCanonicalRoundTrip(t *testing.T) {
	d := identifiers.SHA256([]byte("state"))
	v, err := New(42, d)
	if err != nil {
		t.Fatal(err)
	}
	if v.String() != "rv1:42:"+d.String() || v.ETag() != `"`+v.String()+`"` {
		t.Fatalf("version=%s etag=%s", v.String(), v.ETag())
	}
	p, err := ParseETag(v.ETag())
	if err != nil || p != v {
		t.Fatalf("parsed=%v err=%v", p, err)
	}
	encoded, _ := json.Marshal(v)
	var decoded Version
	if err = json.Unmarshal(encoded, &decoded); err != nil || decoded != v {
		t.Fatalf("decoded=%v err=%v", decoded, err)
	}
}
func TestVersionRejectsNonCanonical(t *testing.T) {
	for _, v := range []string{"", "0", "42", "rv1:0:sha256:0000000000000000000000000000000000000000000000000000000000000000", "rv1:01:sha256:0000000000000000000000000000000000000000000000000000000000000000"} {
		if _, err := Parse(v); err == nil {
			t.Fatalf("accepted %q", v)
		}
	}
}
func TestPrecondition(t *testing.T) {
	d := identifiers.SHA256([]byte("a"))
	v, _ := New(3, d)
	next, _ := v.Next(identifiers.SHA256([]byte("b")))
	if err := MatchVersion(v).Check(true, v); err != nil {
		t.Fatal(err)
	}
	if err := MatchVersion(v).Check(true, next); err == nil {
		t.Fatal("expected conflict")
	}
	if err := (Precondition{MustExist: true, MustNotExist: true}).Validate(); err == nil {
		t.Fatal("expected invalid precondition")
	}
}
