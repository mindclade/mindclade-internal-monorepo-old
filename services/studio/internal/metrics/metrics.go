// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

// Package metrics gives studio a scrape endpoint for the counters it keeps.
//
// # Why pull and not push
//
// The counters this exposes already exist as process-local atomics on hot
// paths, and the values are only meaningful as a RATE over time. A pull
// endpoint reads them where they already live: no export interval to tune, no
// credentials, no background flush that can fail silently, and nothing on the
// request path. Managed Prometheus on GKE scrapes it directly.
//
// # Relationship to libs/go/observability
//
// Collectors return observability.Measurement, so studio speaks the estate's
// metric vocabulary and inherits its name and label validation rather than
// inventing a second one. observability.MetricSink is deliberately NOT
// implemented here: that interface is push-based, and nothing in the estate
// installs a non-nop sink yet. When a shared exporter lands, these collectors
// are what it reads.
package metrics

import (
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"

	"go.mindclade.dev/libs/go/observability"
)

// Collector produces one measurement at scrape time.
type Collector func() observability.Measurement

type registered struct {
	name      string
	help      string
	collector Collector
}

// Registry is the set of collectors this process exposes.
type Registry struct {
	mu         sync.RWMutex
	collectors []registered
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry { return &Registry{} }

// Register adds a collector under name.
//
// The name is validated by producing one measurement immediately: a collector
// that cannot yield a valid measurement is a programming error, and finding it
// at wiring time beats serving a malformed scrape forever.
func (r *Registry) Register(name, help string, collector Collector) error {
	if collector == nil {
		return fmt.Errorf("metrics: collector %q is nil", name)
	}
	sample := collector()
	if sample.Name != name {
		return fmt.Errorf("metrics: collector %q yields measurement named %q", name, sample.Name)
	}
	if err := sample.Validate(); err != nil {
		return fmt.Errorf("metrics: collector %q is invalid: %w", name, err)
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	for _, existing := range r.collectors {
		if existing.name == name {
			return fmt.Errorf("metrics: collector %q is already registered", name)
		}
	}
	r.collectors = append(r.collectors, registered{name: name, help: help, collector: collector})
	return nil
}

// Handler renders the registry in Prometheus text exposition format.
func (r *Registry) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.Method != http.MethodGet && req.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		if req.Method == http.MethodHead {
			w.WriteHeader(http.StatusOK)
			return
		}
		_, _ = w.Write([]byte(r.render()))
	})
}

func (r *Registry) render() string {
	r.mu.RLock()
	collectors := make([]registered, len(r.collectors))
	copy(collectors, r.collectors)
	r.mu.RUnlock()

	// Stable output order. A scrape that reorders between reads is valid
	// Prometheus but makes diffing two scrapes by eye useless.
	sort.Slice(collectors, func(i, j int) bool { return collectors[i].name < collectors[j].name })

	var out strings.Builder
	for _, entry := range collectors {
		measurement := entry.collector()
		name := exposedName(measurement)
		if entry.help != "" {
			fmt.Fprintf(&out, "# HELP %s %s\n", name, escapeHelp(entry.help))
		}
		fmt.Fprintf(&out, "# TYPE %s %s\n", name, exposedType(measurement.Kind))
		fmt.Fprintf(&out, "%s%s %s\n", name, renderLabels(measurement.Labels),
			strconv.FormatFloat(measurement.Value, 'g', -1, 64))
	}
	return out.String()
}

// exposedName maps the estate's dotted metric names onto Prometheus's
// underscore convention, and gives counters the _total suffix its tooling
// expects.
func exposedName(measurement observability.Measurement) string {
	name := strings.ReplaceAll(measurement.Name, ".", "_")
	if measurement.Kind == observability.MetricCounter && !strings.HasSuffix(name, "_total") {
		name += "_total"
	}
	return name
}

func exposedType(kind observability.MetricKind) string {
	switch kind {
	case observability.MetricCounter:
		return "counter"
	case observability.MetricHistogram:
		return "histogram"
	case observability.MetricGauge, observability.MetricUpDownCounter:
		// An up-down counter is a gauge to Prometheus: rate() over something
		// that can decrease is meaningless, and counter would imply it cannot.
		return "gauge"
	default:
		return "untyped"
	}
}

func renderLabels(labels observability.Labels) string {
	if labels.IsZero() {
		return ""
	}
	values := labels.Map()
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	pairs := make([]string, 0, len(keys))
	for _, key := range keys {
		pairs = append(pairs, fmt.Sprintf("%s=%q", strings.ReplaceAll(key, ".", "_"), escapeValue(values[key])))
	}
	return "{" + strings.Join(pairs, ",") + "}"
}

func escapeValue(value string) string {
	return strings.NewReplacer("\\", `\\`, "\n", `\n`, `"`, `\"`).Replace(value)
}

func escapeHelp(help string) string {
	return strings.NewReplacer("\\", `\\`, "\n", `\n`).Replace(help)
}
