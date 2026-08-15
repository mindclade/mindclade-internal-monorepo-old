// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package servicekit

import (
	"runtime/debug"
	"testing"
	"time"
)

func TestBuildInfoFromRuntime(t *testing.T) {
	t.Parallel()

	runtimeInfo := &debug.BuildInfo{
		GoVersion: "go1.23.2",
		Main: debug.Module{
			Path:    "mindclade.internal/services/control_plane",
			Version: "v1.2.3",
		},
		Settings: []debug.BuildSetting{
			{Key: "vcs", Value: "git"},
			{Key: "vcs.revision", Value: "0123456789abcdef"},
			{Key: "vcs.time", Value: "2026-08-12T16:30:00Z"},
			{Key: "vcs.modified", Value: "true"},
		},
	}

	info := buildInfoFromRuntime("control-plane", runtimeInfo)
	if info.Service != "control-plane" || info.Version != "v1.2.3" ||
		info.Revision != "0123456789abcdef" || info.VCS != "git" ||
		!info.Modified || info.GoVersion != "go1.23.2" ||
		info.MainModule != "mindclade.internal/services/control_plane" {
		t.Fatalf("unexpected BuildInfo: %+v", info)
	}
	expectedTime := time.Date(2026, time.August, 12, 16, 30, 0, 0, time.UTC)
	if !info.BuildTime.Equal(expectedTime) {
		t.Fatalf("BuildTime = %v, want %v", info.BuildTime, expectedTime)
	}
	if got := info.ShortRevision(); got != "0123456789ab" {
		t.Fatalf("ShortRevision() = %q", got)
	}

	attributes := info.Attributes()
	if attributes["service.name"] != "control-plane" || attributes["build.modified"] != "true" {
		t.Fatalf("unexpected attributes: %v", attributes)
	}
	attributes["service.name"] = "mutated"
	if info.Attributes()["service.name"] != "control-plane" {
		t.Fatal("Attributes did not return a fresh map")
	}
}

func TestDefaultBuildInfo(t *testing.T) {
	t.Parallel()

	info := buildInfoFromRuntime("", nil)
	if info.Service != unknownBuildValue || info.Version != unknownBuildValue ||
		info.Revision != unknownBuildValue || info.MainModule != unknownBuildValue {
		t.Fatalf("unexpected default BuildInfo: %+v", info)
	}
	if info.ShortRevision() != unknownBuildValue {
		t.Fatalf("ShortRevision() = %q", info.ShortRevision())
	}
}

func TestCurrentBuildInfo(t *testing.T) {
	t.Parallel()

	info := CurrentBuildInfo("runtime-service")
	if info.Service != "runtime-service" {
		t.Fatalf("Service = %q", info.Service)
	}
	if info.GoVersion == "" || info.MainModule == "" {
		t.Fatalf("incomplete current build info: %+v", info)
	}
}
