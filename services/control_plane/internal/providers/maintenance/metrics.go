// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package maintenance

import (
	"context"
	"errors"
	"net"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"go.mindclade.dev/libs/go/clock"
	"go.mindclade.dev/libs/go/faults"
	"go.mindclade.dev/libs/go/httpx"
	"go.mindclade.dev/libs/go/servicekit"
	"go.mindclade.dev/services/control_plane/internal/foundation"
)

const (
	maintenanceMetricsPath                = "/metrics"
	maintenanceMetricsReadHeaderTimeout   = 5 * time.Second
	maintenanceMetricsReadTimeout         = 5 * time.Second
	maintenanceMetricsWriteTimeout        = 10 * time.Second
	maintenanceMetricsIdleTimeout         = 30 * time.Second
	maintenanceMetricsGatherTimeout       = 5 * time.Second
	maintenanceMetricsMaxHeaderBytes      = 64 << 10
	maintenanceMetricsMaxRequestsInFlight = 2

	maintenanceSampleInterval = 15 * time.Second
	maintenanceQueryTimeout   = 2 * time.Second
	maintenanceStaleAfter     = 60 * time.Second
	maintenanceDriftLookback  = 24 * time.Hour
	maintenanceDriftLimit     = 1000
	// The terminal value is an explicit overflow sentinel: 1001 means the
	// actual expired-reservation backlog is at least 1001, not exactly 1001.
	expirationBacklogOverflowSentinel = 1001

	probeExpiration = "expiration"
	probeLineage    = "lineage"

	driftMissingAudit  = "missing_audit"
	driftMissingOutbox = "missing_outbox"
	driftMismatch      = "mismatch"
)

var maintenanceProbeNames = [...]string{probeExpiration, probeLineage}

type maintenanceExpirationSnapshot struct {
	backlog                     int64
	oldestExpiredAt             time.Time
	lastSuccessfulSweep         time.Time
	consecutiveBackloggedSweeps uint8
}

type maintenanceLineageSnapshot struct {
	missingAudit  int64
	missingOutbox int64
	mismatch      int64
}

type maintenanceSnapshotSource interface {
	Expiration(context.Context, time.Time) (maintenanceExpirationSnapshot, error)
	Lineage(context.Context, time.Time) (maintenanceLineageSnapshot, error)
}

type maintenanceMetricState struct {
	expiration            maintenanceExpirationSnapshot
	expirationSucceeded   bool
	expirationLastSuccess time.Time
	lineage               maintenanceLineageSnapshot
	lineageSucceeded      bool
	lineageLastSuccess    time.Time
}

type maintenanceMetricsConfig struct {
	sampleInterval time.Duration
	queryTimeout   time.Duration
	staleAfter     time.Duration
}

func defaultMaintenanceMetricsConfig() maintenanceMetricsConfig {
	return maintenanceMetricsConfig{
		sampleInterval: maintenanceSampleInterval,
		queryTimeout:   maintenanceQueryTimeout,
		staleAfter:     maintenanceStaleAfter,
	}
}

// maintenanceMetricsRuntime owns one private registry and two lifecycle
// components: a scrape server and a background PostgreSQL sampler. Scraping
// reads only the atomically published last-known state and never touches the
// database.
type maintenanceMetricsRuntime struct {
	source   maintenanceSnapshotSource
	clock    clock.Clock
	config   maintenanceMetricsConfig
	state    atomic.Pointer[maintenanceMetricState]
	running  atomic.Bool
	handler  http.Handler
	listener net.Listener
	server   *httpx.Server
}

func newMaintenanceMetrics(
	address string,
	shutdownTimeout time.Duration,
	source maintenanceSnapshotSource,
	value clock.Clock,
) (*maintenanceMetricsRuntime, error) {
	return newMaintenanceMetricsWithConfig(address, shutdownTimeout, source, value, defaultMaintenanceMetricsConfig())
}

func newMaintenanceMetricsWithConfig(
	address string,
	shutdownTimeout time.Duration,
	source maintenanceSnapshotSource,
	value clock.Clock,
	config maintenanceMetricsConfig,
) (*maintenanceMetricsRuntime, error) {
	const operation = "controlplane.maintenance.newMaintenanceMetrics"
	address = strings.TrimSpace(address)
	if address == "" || foundation.IsNil(source) || foundation.IsNil(value) ||
		config.sampleInterval <= 0 || config.queryTimeout <= 0 || config.staleAfter < config.sampleInterval+2*config.queryTimeout {
		return nil, maintenanceFault(faults.CodeInvalidArgument, "maintenance_metrics_configuration_invalid", "maintenance metrics configuration is invalid", operation)
	}

	runtime := &maintenanceMetricsRuntime{source: source, clock: value, config: config}
	runtime.state.Store(&maintenanceMetricState{})
	registry := prometheus.NewRegistry()
	if err := registry.Register(newMaintenanceCollector(&runtime.state, value, config.staleAfter)); err != nil {
		return nil, faults.Wrap(err, faults.CodeInternal, "maintenance metrics could not be registered",
			faults.WithReason("maintenance_metrics_registration_failed"), faults.WithOperation(operation), faults.WithRetryPolicy(faults.NoRetry()))
	}
	exposition := promhttp.HandlerFor(registry, promhttp.HandlerOpts{
		ErrorHandling:       promhttp.HTTPErrorOnError,
		DisableCompression:  true,
		MaxRequestsInFlight: maintenanceMetricsMaxRequestsInFlight,
		Timeout:             maintenanceMetricsGatherTimeout,
	})
	runtime.handler = newMaintenanceMetricsHandler(exposition)
	server, err := httpx.NewServer(runtime.handler, httpx.ServerConfig{
		ReadHeaderTimeout: maintenanceMetricsReadHeaderTimeout,
		ReadTimeout:       maintenanceMetricsReadTimeout,
		WriteTimeout:      maintenanceMetricsWriteTimeout,
		IdleTimeout:       maintenanceMetricsIdleTimeout,
		ShutdownTimeout:   shutdownTimeout,
		MaxHeaderBytes:    maintenanceMetricsMaxHeaderBytes,
	})
	if err != nil {
		return nil, err
	}
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return nil, faults.Wrap(err, faults.CodeUnavailable, "maintenance metrics listener could not be opened",
			faults.WithReason("maintenance_metrics_listener_failed"), faults.WithOperation(operation),
			faults.WithField("address", address), faults.WithRetryPolicy(faults.BackoffRetry(0)))
	}
	runtime.server = server
	runtime.listener = listener
	return runtime, nil
}

func (runtime *maintenanceMetricsRuntime) serverComponent() servicekit.Component {
	component := runtime.server.Component("admission-maintenance-metrics-server", runtime.listener)
	stop := component.Stop
	component.Stop = func(ctx context.Context) error {
		return errors.Join(stop(ctx), closeMaintenanceMetricsListener(runtime.listener))
	}
	return component
}

func (runtime *maintenanceMetricsRuntime) samplerComponent() servicekit.Component {
	return servicekit.Component{
		Name:      "admission-maintenance-metrics-sampler",
		Run:       runtime.runSampler,
		Readiness: runtime.readiness,
	}
}

func (runtime *maintenanceMetricsRuntime) Close() error {
	if runtime == nil {
		return nil
	}
	var serverErr error
	if runtime.server != nil {
		serverErr = runtime.server.Close()
	}
	return errors.Join(serverErr, closeMaintenanceMetricsListener(runtime.listener))
}

func (runtime *maintenanceMetricsRuntime) runSampler(ctx context.Context) error {
	const operation = "controlplane.maintenance.metrics.Run"
	if ctx == nil || runtime == nil || foundation.IsNil(runtime.source) || foundation.IsNil(runtime.clock) {
		return maintenanceFault(faults.CodeFailedPrecondition, "maintenance_metrics_unconfigured", "maintenance metrics sampler is not configured", operation)
	}
	if !runtime.running.CompareAndSwap(false, true) {
		return maintenanceFault(faults.CodeFailedPrecondition, "maintenance_metrics_already_run", "maintenance metrics sampler already ran", operation)
	}
	defer runtime.running.Store(false)
	for {
		if ctx.Err() != nil {
			return nil
		}
		runtime.sample(ctx)
		if err := runtime.clock.Sleep(ctx, runtime.config.sampleInterval); err != nil {
			return nil
		}
	}
}

type expirationProbeResult struct {
	snapshot maintenanceExpirationSnapshot
	err      error
}

type lineageProbeResult struct {
	snapshot maintenanceLineageSnapshot
	err      error
}

func (runtime *maintenanceMetricsRuntime) sample(ctx context.Context) {
	now := runtime.clock.Now().Round(0).UTC()
	expirationCtx, cancelExpiration := context.WithTimeout(ctx, runtime.config.queryTimeout)
	expirationSnapshot, expirationErr := runtime.source.Expiration(expirationCtx, now)
	cancelExpiration()
	expirationUpperBound := runtime.clock.Now().Round(0).UTC()
	if expirationErr == nil {
		expirationErr = validateExpirationSnapshot(expirationSnapshot, expirationUpperBound)
	}
	expirationResult := expirationProbeResult{snapshot: expirationSnapshot, err: expirationErr}

	lineageCtx, cancelLineage := context.WithTimeout(ctx, runtime.config.queryTimeout)
	lineageSnapshot, lineageErr := runtime.source.Lineage(lineageCtx, now)
	cancelLineage()
	if lineageErr == nil {
		lineageErr = validateLineageSnapshot(lineageSnapshot)
	}
	lineageResult := lineageProbeResult{snapshot: lineageSnapshot, err: lineageErr}
	completedAt := runtime.clock.Now().Round(0).UTC()
	previous := runtime.state.Load()
	next := maintenanceMetricState{}
	if previous != nil {
		next = *previous
	}
	if expirationResult.err == nil {
		next.expiration = expirationResult.snapshot
		next.expirationSucceeded = true
		next.expirationLastSuccess = completedAt
	} else {
		next.expirationSucceeded = false
	}
	if lineageResult.err == nil {
		next.lineage = lineageResult.snapshot
		next.lineageSucceeded = true
		next.lineageLastSuccess = completedAt
	} else {
		next.lineageSucceeded = false
	}
	runtime.state.Store(&next)
}

func (runtime *maintenanceMetricsRuntime) readiness(_ context.Context) error {
	if runtime == nil || foundation.IsNil(runtime.clock) {
		return maintenanceFault(faults.CodeFailedPrecondition, "maintenance_metrics_unconfigured", "maintenance metrics sampler is not configured", "controlplane.maintenance.metrics.Readiness")
	}
	state := runtime.state.Load()
	now := runtime.clock.Now().Round(0).UTC()
	if state == nil || !probeSnapshotHealthy(state.expirationSucceeded, state.expirationLastSuccess, now, runtime.config.staleAfter) {
		return faults.New(faults.CodeUnavailable, "maintenance expiration snapshot is failed or stale",
			faults.WithReason("maintenance_metrics_snapshot_stale"), faults.WithOperation("controlplane.maintenance.metrics.Readiness"),
			faults.WithField("probe", probeExpiration), faults.WithRetryPolicy(faults.BackoffRetry(0)))
	}
	if !probeSnapshotHealthy(state.lineageSucceeded, state.lineageLastSuccess, now, runtime.config.staleAfter) {
		return faults.New(faults.CodeUnavailable, "maintenance lineage snapshot is failed or stale",
			faults.WithReason("maintenance_metrics_snapshot_stale"), faults.WithOperation("controlplane.maintenance.metrics.Readiness"),
			faults.WithField("probe", probeLineage), faults.WithRetryPolicy(faults.BackoffRetry(0)))
	}
	return nil
}

func validateExpirationSnapshot(snapshot maintenanceExpirationSnapshot, now time.Time) error {
	if snapshot.consecutiveBackloggedSweeps > 2 ||
		snapshot.backlog < 0 || snapshot.backlog > expirationBacklogOverflowSentinel ||
		(snapshot.backlog > 0 && (snapshot.oldestExpiredAt.IsZero() || snapshot.oldestExpiredAt.After(now))) ||
		(snapshot.backlog == 0 && !snapshot.oldestExpiredAt.IsZero()) ||
		(!snapshot.lastSuccessfulSweep.IsZero() && snapshot.lastSuccessfulSweep.After(now)) {
		return maintenanceFault(faults.CodeDataLoss, "maintenance_expiration_snapshot_invalid", "maintenance expiration snapshot is invalid", "controlplane.maintenance.metrics.validateExpirationSnapshot")
	}
	return nil
}

func validateLineageSnapshot(snapshot maintenanceLineageSnapshot) error {
	for _, value := range []int64{snapshot.missingAudit, snapshot.missingOutbox, snapshot.mismatch} {
		if value < 0 || value > maintenanceDriftLimit {
			return maintenanceFault(faults.CodeDataLoss, "maintenance_lineage_snapshot_invalid", "maintenance lineage snapshot is invalid", "controlplane.maintenance.metrics.validateLineageSnapshot")
		}
	}
	return nil
}

func probeSnapshotHealthy(succeeded bool, lastSuccess, now time.Time, staleAfter time.Duration) bool {
	if !succeeded || lastSuccess.IsZero() || now.Before(lastSuccess) {
		return false
	}
	return now.Sub(lastSuccess) <= staleAfter
}

func newMaintenanceMetricsHandler(exposition http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Cache-Control", "no-store")
		writer.Header().Set("X-Content-Type-Options", "nosniff")
		if request.URL.Path != maintenanceMetricsPath {
			http.NotFound(writer, request)
			return
		}
		switch request.Method {
		case http.MethodGet:
			exposition.ServeHTTP(writer, request)
		case http.MethodHead:
			exposition.ServeHTTP(maintenanceBodySuppressingWriter{ResponseWriter: writer}, request)
		default:
			writer.Header().Set("Allow", "GET, HEAD")
			writer.WriteHeader(http.StatusMethodNotAllowed)
		}
	})
}

type maintenanceBodySuppressingWriter struct {
	http.ResponseWriter
}

func (writer maintenanceBodySuppressingWriter) Write(payload []byte) (int, error) {
	return len(payload), nil
}

func closeMaintenanceMetricsListener(listener net.Listener) error {
	if listener == nil {
		return nil
	}
	err := listener.Close()
	if errors.Is(err, net.ErrClosed) {
		return nil
	}
	return err
}

type maintenanceCollector struct {
	state       *atomic.Pointer[maintenanceMetricState]
	clock       clock.Clock
	staleAfter  time.Duration
	descriptors maintenanceMetricDescriptors
}

type maintenanceMetricDescriptors struct {
	expirationBacklog           *prometheus.Desc
	oldestExpiredAge            *prometheus.Desc
	lastSuccessfulSweep         *prometheus.Desc
	consecutiveBackloggedSweeps *prometheus.Desc
	eventDrift                  *prometheus.Desc
	snapshotSuccess             *prometheus.Desc
	snapshotLastSuccess         *prometheus.Desc
}

func newMaintenanceCollector(state *atomic.Pointer[maintenanceMetricState], value clock.Clock, staleAfter time.Duration) *maintenanceCollector {
	prefix := "mindclade_control_admission_"
	return &maintenanceCollector{
		state: state, clock: value, staleAfter: staleAfter,
		descriptors: maintenanceMetricDescriptors{
			expirationBacklog:           prometheus.NewDesc(prefix+"expiration_backlog", "Bounded count of expired reservations still reserved; 1001 means at least 1001.", nil, nil),
			oldestExpiredAge:            prometheus.NewDesc(prefix+"oldest_expired_reservation_age_seconds", "Age in seconds of the oldest expired reservation awaiting materialization.", nil, nil),
			lastSuccessfulSweep:         prometheus.NewDesc(prefix+"last_successful_sweep_timestamp_seconds", "Unix timestamp of the last successfully completed admission expiration sweep, or zero if none.", nil, nil),
			consecutiveBackloggedSweeps: prometheus.NewDesc(prefix+"consecutive_backlogged_sweeps", "Consecutive completed sweeps that reported remaining backlog, saturated at two.", nil, nil),
			eventDrift:                  prometheus.NewDesc(prefix+"event_drift", "Bounded sampled admission audit/outbox lineage drift by fixed kind.", []string{"kind"}, nil),
			snapshotSuccess:             prometheus.NewDesc(prefix+"snapshot_success", "Whether the last probe snapshot succeeded and remains fresh.", []string{"probe"}, nil),
			snapshotLastSuccess:         prometheus.NewDesc(prefix+"snapshot_last_success_timestamp_seconds", "Unix timestamp of the last successful probe snapshot, or zero if none.", []string{"probe"}, nil),
		},
	}
}

func (collector *maintenanceCollector) Describe(output chan<- *prometheus.Desc) {
	output <- collector.descriptors.expirationBacklog
	output <- collector.descriptors.oldestExpiredAge
	output <- collector.descriptors.lastSuccessfulSweep
	output <- collector.descriptors.consecutiveBackloggedSweeps
	output <- collector.descriptors.eventDrift
	output <- collector.descriptors.snapshotSuccess
	output <- collector.descriptors.snapshotLastSuccess
}

func (collector *maintenanceCollector) Collect(output chan<- prometheus.Metric) {
	state := collector.state.Load()
	if state == nil {
		state = &maintenanceMetricState{}
	}
	now := collector.clock.Now().Round(0).UTC()
	oldestAge := 0.0
	if state.expiration.backlog > 0 {
		oldestAge = max(0, now.Sub(state.expiration.oldestExpiredAt).Seconds())
	}
	output <- prometheus.MustNewConstMetric(collector.descriptors.expirationBacklog, prometheus.GaugeValue, float64(state.expiration.backlog))
	output <- prometheus.MustNewConstMetric(collector.descriptors.oldestExpiredAge, prometheus.GaugeValue, oldestAge)
	output <- prometheus.MustNewConstMetric(collector.descriptors.lastSuccessfulSweep, prometheus.GaugeValue, unixSeconds(state.expiration.lastSuccessfulSweep))
	output <- prometheus.MustNewConstMetric(collector.descriptors.consecutiveBackloggedSweeps, prometheus.GaugeValue, float64(state.expiration.consecutiveBackloggedSweeps))
	for _, drift := range []struct {
		kind  string
		value int64
	}{
		{driftMissingAudit, state.lineage.missingAudit},
		{driftMissingOutbox, state.lineage.missingOutbox},
		{driftMismatch, state.lineage.mismatch},
	} {
		output <- prometheus.MustNewConstMetric(collector.descriptors.eventDrift, prometheus.GaugeValue, float64(drift.value), drift.kind)
	}
	for _, probe := range maintenanceProbeNames {
		succeeded, lastSuccess := state.expirationSucceeded, state.expirationLastSuccess
		if probe == probeLineage {
			succeeded, lastSuccess = state.lineageSucceeded, state.lineageLastSuccess
		}
		healthy := 0.0
		if probeSnapshotHealthy(succeeded, lastSuccess, now, collector.staleAfter) {
			healthy = 1
		}
		output <- prometheus.MustNewConstMetric(collector.descriptors.snapshotSuccess, prometheus.GaugeValue, healthy, probe)
		output <- prometheus.MustNewConstMetric(collector.descriptors.snapshotLastSuccess, prometheus.GaugeValue, unixSeconds(lastSuccess), probe)
	}
}

func unixSeconds(value time.Time) float64 {
	if value.IsZero() {
		return 0
	}
	return float64(value.UnixNano()) / float64(time.Second)
}

var _ prometheus.Collector = (*maintenanceCollector)(nil)
