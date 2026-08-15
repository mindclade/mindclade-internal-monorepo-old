// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package config

import (
	"context"
	"os"
	"sort"
	"strings"
)

type Source interface {
	Name() string
	Load(context.Context) (map[string]string, error)
}
type SourceFunc struct {
	SourceName string
	LoadFunc   func(context.Context) (map[string]string, error)
}

func (source SourceFunc) Name() string { return source.SourceName }
func (source SourceFunc) Load(ctx context.Context) (map[string]string, error) {
	return source.LoadFunc(ctx)
}

type MapSource struct {
	SourceName string
	Values     map[string]string
}

func (source MapSource) Name() string {
	if strings.TrimSpace(source.SourceName) == "" {
		return "map"
	}
	return strings.TrimSpace(source.SourceName)
}
func (source MapSource) Load(context.Context) (map[string]string, error) {
	result := make(map[string]string, len(source.Values))
	for key, value := range source.Values {
		result[key] = value
	}
	return result, nil
}

// EnvSource maps logical config keys to exact environment variable names. It
// never scans the process environment, preventing accidental unknown-key or
// secret capture.
type EnvSource struct {
	SourceName string
	Mapping    map[string]string
	Lookup     func(string) (string, bool)
}

func (source EnvSource) Name() string {
	if strings.TrimSpace(source.SourceName) == "" {
		return "environment"
	}
	return strings.TrimSpace(source.SourceName)
}
func (source EnvSource) Load(context.Context) (map[string]string, error) {
	lookup := source.Lookup
	if lookup == nil {
		lookup = os.LookupEnv
	}
	keys := make([]string, 0, len(source.Mapping))
	for key := range source.Mapping {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	result := make(map[string]string)
	for _, key := range keys {
		if value, ok := lookup(source.Mapping[key]); ok {
			result[key] = value
		}
	}
	return result, nil
}
