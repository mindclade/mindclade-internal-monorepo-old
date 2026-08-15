// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package identifiers

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestSHA256KnownVector(t *testing.T) {
	t.Parallel()

	digest := SHA256String("abc")
	want := "sha256:ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad"
	if got := digest.String(); got != want {
		t.Fatalf("String() = %q", got)
	}
	parsed, err := ParseDigest(want)
	if err != nil {
		t.Fatal(err)
	}
	if !parsed.Equal(digest) {
		t.Fatalf("parsed digest differs")
	}
}

func TestSHA256Reader(t *testing.T) {
	t.Parallel()

	digest, count, err := SHA256Reader(strings.NewReader("abc"))
	if err != nil {
		t.Fatal(err)
	}
	if count != 3 || !digest.Equal(SHA256String("abc")) {
		t.Fatalf("count=%d digest=%s", count, digest)
	}
	if _, _, err := SHA256Reader(nil); !errors.Is(err, ErrInvalidDigest) {
		t.Fatalf("SHA256Reader(nil) error = %v", err)
	}
}

func TestParseDigestRejectsNonCanonicalValues(t *testing.T) {
	t.Parallel()

	valid := SHA256String("abc").String()
	values := []string{
		"",
		strings.TrimPrefix(valid, "sha256:"),
		"SHA256:" + strings.TrimPrefix(valid, "sha256:"),
		"sha256:" + strings.ToUpper(strings.TrimPrefix(valid, "sha256:")),
		"sha256:" + strings.Repeat("z", DigestHexLength),
	}
	for _, value := range values {
		_, err := ParseDigest(value)
		if !errors.Is(err, ErrInvalidDigest) {
			t.Fatalf("ParseDigest(%q) error = %v", value, err)
		}
	}
}

func TestDigestSerializationAndSQL(t *testing.T) {
	t.Parallel()

	original := SHA256String("artifact")
	encoded, err := json.Marshal(original)
	if err != nil {
		t.Fatal(err)
	}
	var decoded Digest
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	if !decoded.Equal(original) {
		t.Fatalf("json round trip = %s", decoded)
	}

	var scanned Digest
	if err := scanned.Scan(original.Bytes()); err != nil {
		t.Fatal(err)
	}
	if !scanned.Equal(original) {
		t.Fatalf("binary scan = %s", scanned)
	}
	if err := scanned.Scan(original.String()); err != nil {
		t.Fatal(err)
	}
	if !scanned.Equal(original) {
		t.Fatalf("text scan = %s", scanned)
	}

	value, err := original.Value()
	if err != nil || value != original.String() {
		t.Fatalf("Value() = %v, %v", value, err)
	}

	var zero Digest
	zeroJSON, err := json.Marshal(zero)
	if err != nil || !bytes.Equal(zeroJSON, []byte("null")) {
		t.Fatalf("zero JSON = %s, %v", zeroJSON, err)
	}
}

func TestDigestBytesAreDefensiveCopy(t *testing.T) {
	t.Parallel()

	digest := SHA256String("abc")
	value := digest.Bytes()
	value[0] ^= 0xFF
	mutated, err := DigestFromBytes(value)
	if err != nil {
		t.Fatal(err)
	}
	if digest.Equal(mutated) {
		t.Fatal("mutating Bytes() output changed digest")
	}
}

func TestDigestPresenceAndVerification(t *testing.T) {
	t.Parallel()

	payload := []byte("payload")
	digest := SHA256(payload)
	if !digest.Valid() || !digest.Verify(payload) || digest.Verify([]byte("other")) {
		t.Fatal("digest verification state is incorrect")
	}

	var absent Digest
	if absent.Valid() || absent.Verify(payload) || !absent.Equal(Digest{}) {
		t.Fatal("absent digest state is incorrect")
	}

	allZero, err := DigestFromBytes(make([]byte, DigestBinarySize))
	if err != nil {
		t.Fatal(err)
	}
	if allZero.IsZero() || !allZero.Valid() || len(allZero.Bytes()) != DigestBinarySize {
		t.Fatal("present all-zero digest was confused with absence")
	}
}
