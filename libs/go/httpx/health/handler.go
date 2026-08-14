// Copyright 2026 Mindclade. All rights reserved.
// Confidential, proprietary, and trade-secret information.

package health

import (
	"context"
	"net/http"
	"time"

	"mindclade.internal/libs/go/httpx"
	"mindclade.internal/libs/go/servicekit"
)

type Prober interface {
	Liveness(context.Context) servicekit.ProbeReport
	Readiness(context.Context) servicekit.ProbeReport
}

type Config struct {
	LivenessPath  string
	ReadinessPath string
}

type response struct {
	Status    string    `json:"status"`
	CheckedAt time.Time `json:"checked_at"`
	Duration  string    `json:"duration"`
	Checks    []check   `json:"checks,omitempty"`
}
type check struct {
	Name     string `json:"name"`
	Status   string `json:"status"`
	Duration string `json:"duration,omitempty"`
}

func NewHandler(prober Prober, config Config) http.Handler {
	if config.LivenessPath == "" {
		config.LivenessPath = "/livez"
	}
	if config.ReadinessPath == "" {
		config.ReadinessPath = "/readyz"
	}
	mux := http.NewServeMux()
	mux.Handle(config.LivenessPath, probeHandler(func(ctx context.Context) servicekit.ProbeReport {
		if prober == nil {
			return failedReport()
		}
		return prober.Liveness(ctx)
	}))
	mux.Handle(config.ReadinessPath, probeHandler(func(ctx context.Context) servicekit.ProbeReport {
		if prober == nil {
			return failedReport()
		}
		return prober.Readiness(ctx)
	}))
	return mux
}

func probeHandler(probe func(context.Context) servicekit.ProbeReport) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodGet && request.Method != http.MethodHead {
			writer.Header().Set("Allow", "GET, HEAD")
			writer.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		report := probe(request.Context())
		status := http.StatusOK
		state := "ok"
		if !report.OK {
			status = http.StatusServiceUnavailable
			state = "failed"
		}
		payload := response{Status: state, CheckedAt: report.CheckedAt, Duration: report.Duration.String()}
		for _, result := range report.Results {
			checkState := "ok"
			if !result.OK {
				checkState = "failed"
			}
			payload.Checks = append(payload.Checks, check{Name: result.Name, Status: checkState, Duration: result.Duration.String()})
		}
		writer.Header().Set("Cache-Control", "no-store")
		if request.Method == http.MethodHead {
			writer.WriteHeader(status)
			return
		}
		_ = httpx.WriteJSON(writer, status, payload)
	})
}

func failedReport() servicekit.ProbeReport {
	now := time.Now()
	return servicekit.ProbeReport{OK: false, CheckedAt: now, Results: []servicekit.ProbeResult{{Name: "health", OK: false, CheckedAt: now}}}
}
