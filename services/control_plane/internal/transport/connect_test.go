// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package transport

import (
	"net/http"
	"testing"

	"mindclade.internal/libs/go/faults"
	"mindclade.internal/libs/go/servicekit/production"
)

type recordingMux struct {
	paths []string
}

func (mux *recordingMux) Handle(path string, _ http.Handler) {
	mux.paths = append(mux.paths, path)
}

func TestMountConnect(t *testing.T) {
	mux := &recordingMux{}
	capability, err := MountConnect(mux, ConnectMount{
		Path:    "/mindclade.test.v1.TestService/",
		Handler: http.NotFoundHandler(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if capability != production.CapabilityConnect || len(mux.paths) != 1 {
		t.Fatalf("capability=%q paths=%v", capability, mux.paths)
	}
}

func TestMountConnectRejectsEmpty(t *testing.T) {
	_, err := MountConnect(&recordingMux{})
	if err == nil || faults.ReasonOf(err) != "empty_connect_mounts" {
		t.Fatalf("err=%v", err)
	}
}
