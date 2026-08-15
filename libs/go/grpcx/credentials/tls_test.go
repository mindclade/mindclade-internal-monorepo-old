// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package credentials

import (
	"crypto/tls"
	"crypto/x509"
	"testing"
)

func TestClientRequiresServerName(t *testing.T) {
	if _, err := NewClient(ClientConfig{RootCAs: x509.NewCertPool()}); err == nil {
		t.Fatal("expected error")
	}
}

func TestCloneCertificateDoesNotAliasWireSlices(t *testing.T) {
	source := tls.Certificate{
		Certificate:                 [][]byte{{1, 2, 3}},
		OCSPStaple:                  []byte{4, 5},
		SignedCertificateTimestamps: [][]byte{{6, 7}},
	}
	cloned := cloneCertificate(source)
	source.Certificate[0][0] = 9
	source.OCSPStaple[0] = 9
	source.SignedCertificateTimestamps[0][0] = 9
	if cloned.Certificate[0][0] != 1 || cloned.OCSPStaple[0] != 4 || cloned.SignedCertificateTimestamps[0][0] != 6 {
		t.Fatalf("clone aliases source: %#v", cloned)
	}
}
