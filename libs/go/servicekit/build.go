// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package servicekit

import (
	"runtime"
	"runtime/debug"
	"strconv"
	"strings"
	"time"
)

const unknownBuildValue = "unknown"

// BuildInfo is normalized runtime build provenance for service diagnostics.
type BuildInfo struct {
	Service    string
	Version    string
	Revision   string
	VCS        string
	BuildTime  time.Time
	Modified   bool
	GoVersion  string
	MainModule string
}

// CurrentBuildInfo reads the build metadata embedded by the Go toolchain.
func CurrentBuildInfo(service string) BuildInfo {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return defaultBuildInfo(service)
	}
	return buildInfoFromRuntime(service, info)
}

func buildInfoFromRuntime(service string, info *debug.BuildInfo) BuildInfo {
	result := defaultBuildInfo(service)
	if info == nil {
		return result
	}

	if value := strings.TrimSpace(info.GoVersion); value != "" {
		result.GoVersion = value
	}
	if value := strings.TrimSpace(info.Main.Path); value != "" {
		result.MainModule = value
	}
	if value := strings.TrimSpace(info.Main.Version); value != "" && value != "(devel)" {
		result.Version = value
	}

	for _, setting := range info.Settings {
		value := strings.TrimSpace(setting.Value)
		switch setting.Key {
		case "vcs":
			if value != "" {
				result.VCS = value
			}
		case "vcs.revision":
			if value != "" {
				result.Revision = value
			}
		case "vcs.time":
			if parsed, err := time.Parse(time.RFC3339, value); err == nil {
				result.BuildTime = parsed.UTC()
			}
		case "vcs.modified":
			if parsed, err := strconv.ParseBool(value); err == nil {
				result.Modified = parsed
			}
		}
	}
	return result
}

func defaultBuildInfo(service string) BuildInfo {
	normalizedService := strings.TrimSpace(service)
	if normalizedService == "" {
		normalizedService = unknownBuildValue
	}
	return BuildInfo{
		Service:    normalizedService,
		Version:    unknownBuildValue,
		Revision:   unknownBuildValue,
		VCS:        unknownBuildValue,
		GoVersion:  runtime.Version(),
		MainModule: unknownBuildValue,
	}
}

// ShortRevision returns a display-safe revision prefix.
func (info BuildInfo) ShortRevision() string {
	revision := strings.TrimSpace(info.Revision)
	if revision == "" {
		return unknownBuildValue
	}
	if len(revision) <= 12 {
		return revision
	}
	return revision[:12]
}

// Attributes returns a newly allocated set of stable diagnostic attributes.
func (info BuildInfo) Attributes() map[string]string {
	builtAt := unknownBuildValue
	if !info.BuildTime.IsZero() {
		builtAt = info.BuildTime.UTC().Format(time.RFC3339)
	}
	return map[string]string{
		"service.name":       valueOrUnknown(info.Service),
		"service.version":    valueOrUnknown(info.Version),
		"build.revision":     valueOrUnknown(info.Revision),
		"build.vcs":          valueOrUnknown(info.VCS),
		"build.time":         builtAt,
		"build.modified":     strconv.FormatBool(info.Modified),
		"runtime.go.version": valueOrUnknown(info.GoVersion),
		"go.main.module":     valueOrUnknown(info.MainModule),
	}
}

func valueOrUnknown(value string) string {
	if normalized := strings.TrimSpace(value); normalized != "" {
		return normalized
	}
	return unknownBuildValue
}
