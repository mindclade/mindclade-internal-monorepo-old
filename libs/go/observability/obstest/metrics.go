// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package obstest

import (
	"context"
	"sync"

	"mindclade.internal/libs/go/observability"
)

// MetricRecorder is a concurrency-safe in-memory MetricSink.
type MetricRecorder struct {
	mu           sync.Mutex
	measurements []observability.Measurement
}

func (recorder *MetricRecorder) Record(_ context.Context, measurement observability.Measurement) {
	if recorder == nil {
		return
	}
	measurement.Labels = cloneLabels(measurement.Labels)
	recorder.mu.Lock()
	recorder.measurements = append(recorder.measurements, measurement)
	recorder.mu.Unlock()
}

func (recorder *MetricRecorder) Measurements() []observability.Measurement {
	if recorder == nil {
		return nil
	}
	recorder.mu.Lock()
	output := make([]observability.Measurement, len(recorder.measurements))
	copy(output, recorder.measurements)
	for index := range output {
		output[index].Labels = cloneLabels(output[index].Labels)
	}
	recorder.mu.Unlock()
	return output
}

func (recorder *MetricRecorder) Reset() {
	if recorder == nil {
		return
	}
	recorder.mu.Lock()
	recorder.measurements = nil
	recorder.mu.Unlock()
}

func cloneLabels(labels observability.Labels) observability.Labels {
	cloned, _ := observability.NewLabels(labels.Map())
	return cloned
}
