// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package admissionmetrics

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"go.mindclade.dev/libs/go/faults"
)

func TestResultTaxonomyIsCompleteAndBounded(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want string
	}{
		{name: "allow", want: resultAllow},
		{name: "permission_denied", err: testFault(faults.CodePermissionDenied), want: resultDeny},
		{name: "unauthenticated", err: testFault(faults.CodeUnauthenticated), want: resultDeny},
		{name: "exhausted", err: testFault(faults.CodeResourceExhausted), want: resultExhausted},
		{name: "already_exists", err: testFault(faults.CodeAlreadyExists), want: resultConflict},
		{name: "failed_precondition", err: testFault(faults.CodeFailedPrecondition), want: resultConflict},
		{name: "conflict", err: testFault(faults.CodeConflict), want: resultConflict},
		{name: "aborted", err: testFault(faults.CodeAborted), want: resultConflict},
		{name: "not_found", err: testFault(faults.CodeNotFound), want: resultNotFound},
		{name: "unavailable", err: testFault(faults.CodeUnavailable), want: resultUnavailable},
		{name: "canceled", err: context.Canceled, want: resultCanceled},
		{name: "deadline", err: context.DeadlineExceeded, want: resultDeadline},
		{name: "invalid", err: testFault(faults.CodeInvalidArgument), want: resultInvalid},
		{name: "out_of_range", err: testFault(faults.CodeOutOfRange), want: resultInvalid},
		{name: "unknown", err: errors.New("unstructured"), want: resultInternal},
		{name: "not_implemented", err: testFault(faults.CodeNotImplemented), want: resultInternal},
		{name: "internal", err: testFault(faults.CodeInternal), want: resultInternal},
		{name: "data_loss", err: testFault(faults.CodeDataLoss), want: resultInternal},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			if got := classifyResult(test.err); got != test.want {
				t.Fatalf("classifyResult() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestMetricSeriesInventoryIsExact(t *testing.T) {
	runtime := newTestRuntime(t)
	body := scrape(t, runtime.handler, http.MethodGet, metricsPath)
	pattern := regexp.MustCompile(`^mindclade_control_admission_decisions_total\{operation="([a-z_]+)",result="([a-z_]+)"\} 0$`)
	operationPattern := regexp.MustCompile(`operation="([a-z_]+)"`)
	type histogramInventory struct{ buckets, counts, sums int }
	seen := make(map[string]struct{})
	histograms := make(map[string]histogramInventory)
	for _, line := range strings.Split(body, "\n") {
		if line == "" || strings.HasPrefix(line, "# ") {
			continue
		}
		if !strings.HasPrefix(line, "mindclade_control_admission_") {
			t.Fatalf("private registry exported unexpected series %q", line)
		}
		switch {
		case strings.HasPrefix(line, "mindclade_control_admission_decisions_total{"):
			match := pattern.FindStringSubmatch(line)
			if len(match) != 3 {
				t.Fatalf("unexpected decision series identity %q", line)
			}
			seen[match[1]+"/"+match[2]] = struct{}{}
		case strings.HasPrefix(line, "mindclade_control_admission_decision_duration_seconds_bucket{"):
			operation := metricOperation(t, operationPattern, line)
			inventory := histograms[operation]
			inventory.buckets++
			histograms[operation] = inventory
		case strings.HasPrefix(line, "mindclade_control_admission_decision_duration_seconds_count{"):
			operation := metricOperation(t, operationPattern, line)
			inventory := histograms[operation]
			inventory.counts++
			histograms[operation] = inventory
		case strings.HasPrefix(line, "mindclade_control_admission_decision_duration_seconds_sum{"):
			operation := metricOperation(t, operationPattern, line)
			inventory := histograms[operation]
			inventory.sums++
			histograms[operation] = inventory
		default:
			t.Fatalf("unexpected admission metric series %q", line)
		}
	}
	if len(seen) != len(operations)*len(results) {
		t.Fatalf("decision series count = %d, want %d", len(seen), len(operations)*len(results))
	}
	for _, operation := range operations {
		for _, result := range results {
			if _, ok := seen[string(operation)+"/"+result]; !ok {
				t.Fatalf("missing bounded decision series %s/%s", operation, result)
			}
		}
		inventory := histograms[string(operation)]
		if inventory.buckets != 12 || inventory.counts != 1 || inventory.sums != 1 {
			t.Fatalf("%s histogram inventory buckets/count/sum = %d/%d/%d, want 12/1/1", operation, inventory.buckets, inventory.counts, inventory.sums)
		}
	}
	if len(histograms) != len(operations) {
		t.Fatalf("histogram operations = %v, want exactly %v", histograms, operations)
	}
}

func metricOperation(t *testing.T, pattern *regexp.Regexp, line string) string {
	t.Helper()
	match := pattern.FindStringSubmatch(line)
	if len(match) != 2 {
		t.Fatalf("metric has no bounded operation label %q", line)
	}
	return match[1]
}

func TestQualifiedBoundaryIncludesParsingAndResponseCompletionLatency(t *testing.T) {
	runtime := newTestRuntime(t)
	sentinel := faults.New(faults.CodeConflict, "sensitive-cause-9f2d",
		faults.WithReason("sensitive-reason-82b1"),
		faults.WithField("workspace", "sensitive-workspace-d438"))
	handler := runtime.Middleware(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		// Simulate structural request decoding before qualification.
		time.Sleep(12 * time.Millisecond)
		runtime.Qualify(request.Context(), OperationAdmit)
		runtime.Complete(request.Context(), sentinel)
		// Simulate problem-response serialization after terminal classification.
		time.Sleep(12 * time.Millisecond)
		writer.WriteHeader(http.StatusConflict)
	}))
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/admit", nil))

	body := scrape(t, runtime.handler, http.MethodGet, metricsPath)
	if !strings.Contains(body, `mindclade_control_admission_decisions_total{operation="admit",result="conflict"} 1`) {
		t.Fatalf("terminal conflict was not recorded:\n%s", body)
	}
	seconds := metricValue(t, body, `mindclade_control_admission_decision_duration_seconds_sum\{operation="admit"\}`)
	if seconds < 0.020 {
		t.Fatalf("boundary duration = %f, want parsing and response completion included", seconds)
	}
	for _, forbidden := range []string{
		"sensitive-cause-9f2d", "sensitive-reason-82b1", "sensitive-workspace-d438",
		"tenant=", "workspace=", "subject=", "model=", "provider=", "route=", "reason=", "request=", "reservation=", "idempotency=",
	} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("metrics leaked forbidden cardinality or data %q:\n%s", forbidden, body)
		}
	}
}

func TestCallerCanceledIsDistinctAndHasNoLatencySample(t *testing.T) {
	runtime := newTestRuntime(t)
	handler := runtime.Middleware(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		ctx, cancel := context.WithCancel(request.Context())
		cancel()
		runtime.Qualify(ctx, OperationCommit)
		runtime.Complete(ctx, context.Canceled)
		writer.WriteHeader(http.StatusRequestTimeout)
	}))
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/commit", nil))

	body := scrape(t, runtime.handler, http.MethodGet, metricsPath)
	if !strings.Contains(body, `mindclade_control_admission_decisions_total{operation="commit",result="canceled"} 1`) {
		t.Fatalf("caller cancellation was not recorded distinctly:\n%s", body)
	}
	if !strings.Contains(body, `mindclade_control_admission_decision_duration_seconds_count{operation="commit"} 0`) {
		t.Fatalf("caller cancellation contributed a latency sample:\n%s", body)
	}
}

func TestServerDeadlineIsFailureWithLatencySample(t *testing.T) {
	runtime := newTestRuntime(t)
	handler := runtime.Middleware(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		runtime.Qualify(request.Context(), OperationRelease)
		runtime.Complete(request.Context(), context.DeadlineExceeded)
		writer.WriteHeader(http.StatusGatewayTimeout)
	}))
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/release", nil))

	body := scrape(t, runtime.handler, http.MethodGet, metricsPath)
	if !strings.Contains(body, `mindclade_control_admission_decisions_total{operation="release",result="deadline"} 1`) {
		t.Fatalf("server deadline was not recorded:\n%s", body)
	}
	if !strings.Contains(body, `mindclade_control_admission_decision_duration_seconds_count{operation="release"} 1`) {
		t.Fatalf("server deadline omitted its latency sample:\n%s", body)
	}
}

func TestUnqualifiedRequestIsExcluded(t *testing.T) {
	runtime := newTestRuntime(t)
	handler := runtime.Middleware(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusBadRequest)
	}))
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/malformed", nil))
	body := scrape(t, runtime.handler, http.MethodGet, metricsPath)
	for _, operation := range operations {
		if !strings.Contains(body, fmt.Sprintf(`mindclade_control_admission_decision_duration_seconds_count{operation=%q} 0`, operation)) {
			t.Fatalf("unqualified request contributed latency for %s:\n%s", operation, body)
		}
	}
	if strings.Contains(body, `mindclade_control_admission_decisions_total{operation="admit",result="invalid"} 1`) {
		t.Fatalf("unqualified malformed request contributed a decision:\n%s", body)
	}
}

func TestNestedMiddlewareAndRepeatedCompletionRecordOnce(t *testing.T) {
	runtime := newTestRuntime(t)
	handler := runtime.Middleware(runtime.Middleware(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		runtime.Qualify(request.Context(), OperationAdmit)
		runtime.Complete(request.Context(), nil)
		runtime.Complete(request.Context(), testFault(faults.CodeUnavailable))
		writer.WriteHeader(http.StatusServiceUnavailable)
	})))
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/admit", nil))
	body := scrape(t, runtime.handler, http.MethodGet, metricsPath)
	if !strings.Contains(body, `mindclade_control_admission_decisions_total{operation="admit",result="unavailable"} 1`) ||
		!strings.Contains(body, `mindclade_control_admission_decisions_total{operation="admit",result="allow"} 0`) {
		t.Fatalf("terminal result did not supersede without double counting:\n%s", body)
	}
	if !strings.Contains(body, `mindclade_control_admission_decision_duration_seconds_count{operation="admit"} 1`) {
		t.Fatalf("nested middleware double counted duration:\n%s", body)
	}
}

func TestQualifiedPanicIsInternalAndStillPropagates(t *testing.T) {
	runtime := newTestRuntime(t)
	handler := runtime.Middleware(http.HandlerFunc(func(_ http.ResponseWriter, request *http.Request) {
		runtime.Qualify(request.Context(), OperationAdmit)
		panic("sentinel-panic")
	}))
	var recovered any
	func() {
		defer func() { recovered = recover() }()
		handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/admit", nil))
	}()
	if recovered != "sentinel-panic" {
		t.Fatalf("panic = %v, want sentinel", recovered)
	}
	body := scrape(t, runtime.handler, http.MethodGet, metricsPath)
	if !strings.Contains(body, `mindclade_control_admission_decisions_total{operation="admit",result="internal"} 1`) {
		t.Fatalf("panic was not counted as internal:\n%s", body)
	}
}

func TestMiddlewarePreservesResponseWriterCapabilitiesAndForwarding(t *testing.T) {
	runtime := newTestRuntime(t)
	underlying := &capabilityResponseWriter{header: make(http.Header)}
	handler := runtime.Middleware(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		unwrapper, ok := writer.(interface{ Unwrap() http.ResponseWriter })
		if !ok || unwrapper.Unwrap() != underlying {
			t.Fatal("observer does not expose the exact underlying writer")
		}
		flusher, ok := writer.(http.Flusher)
		if !ok {
			t.Fatal("Flusher capability was lost")
		}
		flusher.Flush()
		if !underlying.flushed {
			t.Fatal("Flush was not forwarded")
		}
		hijacker, ok := writer.(http.Hijacker)
		if !ok {
			t.Fatal("Hijacker capability was lost")
		}
		if _, _, err := hijacker.Hijack(); err == nil || !underlying.hijacked {
			t.Fatalf("Hijack was not forwarded: %v", err)
		}
		pusher, ok := writer.(http.Pusher)
		if !ok {
			t.Fatal("Pusher capability was lost")
		}
		if err := pusher.Push("/bounded", nil); err != nil || underlying.pushed != "/bounded" {
			t.Fatalf("Push was not forwarded: %v", err)
		}
		readerFrom, ok := writer.(io.ReaderFrom)
		if !ok {
			t.Fatal("ReaderFrom capability was lost")
		}
		if count, err := readerFrom.ReadFrom(strings.NewReader("bounded")); err != nil || count != 7 || underlying.read != 7 {
			t.Fatalf("ReadFrom was not forwarded: count=%d underlying=%d err=%v", count, underlying.read, err)
		}
		runtime.Qualify(request.Context(), OperationAdmit)
		runtime.Complete(request.Context(), nil)
	}))
	handler.ServeHTTP(underlying, httptest.NewRequest(http.MethodPost, "/admit", nil))
}

func TestMiddlewareDoesNotAddResponseWriterCapabilities(t *testing.T) {
	runtime := newTestRuntime(t)
	underlying := &basicResponseWriter{header: make(http.Header)}
	handler := runtime.Middleware(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		unwrapper, ok := writer.(interface{ Unwrap() http.ResponseWriter })
		if !ok || unwrapper.Unwrap() != underlying {
			t.Fatal("observer does not expose the exact underlying writer")
		}
		if _, ok := writer.(http.Flusher); ok {
			t.Fatal("observer added unsupported Flusher")
		}
		if _, ok := writer.(http.Hijacker); ok {
			t.Fatal("observer added unsupported Hijacker")
		}
		if _, ok := writer.(http.Pusher); ok {
			t.Fatal("observer added unsupported Pusher")
		}
		if _, ok := writer.(io.ReaderFrom); ok {
			t.Fatal("observer added unsupported ReaderFrom")
		}
		runtime.Qualify(request.Context(), OperationAdmit)
		runtime.Complete(request.Context(), nil)
		writer.WriteHeader(http.StatusNoContent)
	}))
	handler.ServeHTTP(underlying, httptest.NewRequest(http.MethodPost, "/admit", nil))
}

func TestTerminalHTTPStatusFallbackUsesBoundedTaxonomy(t *testing.T) {
	cases := []struct {
		name   string
		status int
		result string
	}{
		{name: "allow", status: http.StatusNoContent, result: resultAllow},
		{name: "invalid", status: http.StatusUnprocessableEntity, result: resultInvalid},
		{name: "deny", status: http.StatusForbidden, result: resultDeny},
		{name: "not found", status: http.StatusNotFound, result: resultNotFound},
		{name: "canceled", status: http.StatusRequestTimeout, result: resultCanceled},
		{name: "conflict", status: http.StatusPreconditionFailed, result: resultConflict},
		{name: "exhausted", status: http.StatusTooManyRequests, result: resultExhausted},
		{name: "unavailable", status: http.StatusServiceUnavailable, result: resultUnavailable},
		{name: "deadline", status: http.StatusGatewayTimeout, result: resultDeadline},
		{name: "internal", status: http.StatusInternalServerError, result: resultInternal},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			runtime := newTestRuntime(t)
			handler := runtime.Middleware(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				runtime.Qualify(request.Context(), OperationAdmit)
				if test.result == resultAllow {
					runtime.Complete(request.Context(), nil)
				}
				writer.WriteHeader(test.status)
			}))
			handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/admit", nil))
			body := scrape(t, runtime.handler, http.MethodGet, metricsPath)
			want := fmt.Sprintf(`mindclade_control_admission_decisions_total{operation="admit",result=%q} 1`, test.result)
			if !strings.Contains(body, want) {
				t.Fatalf("terminal status %d missing %q:\n%s", test.status, want, body)
			}
		})
	}
}

func TestQualifiedIncompleteSuccessIsInternal(t *testing.T) {
	runtime := newTestRuntime(t)
	handler := runtime.Middleware(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		runtime.Qualify(request.Context(), OperationAdmit)
		writer.WriteHeader(http.StatusOK)
	}))
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/admit", nil))
	body := scrape(t, runtime.handler, http.MethodGet, metricsPath)
	if !strings.Contains(body, `mindclade_control_admission_decisions_total{operation="admit",result="internal"} 1`) {
		t.Fatalf("qualified incomplete response was not internal:\n%s", body)
	}
}

func TestMiddlewareIsConcurrentSafe(t *testing.T) {
	runtime := newTestRuntime(t)
	handler := runtime.Middleware(http.HandlerFunc(func(_ http.ResponseWriter, request *http.Request) {
		runtime.Qualify(request.Context(), OperationAdmit)
		runtime.Complete(request.Context(), nil)
	}))
	const calls = 64
	var group sync.WaitGroup
	for index := 0; index < calls; index++ {
		group.Add(1)
		go func() {
			defer group.Done()
			handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/admit", nil))
		}()
	}
	group.Wait()
	body := scrape(t, runtime.handler, http.MethodGet, metricsPath)
	want := fmt.Sprintf(`mindclade_control_admission_decisions_total{operation="admit",result="allow"} %d`, calls)
	if !strings.Contains(body, want) {
		t.Fatalf("concurrent count missing %q:\n%s", want, body)
	}
}

func TestMetricsHandlerAllowsOnlyExactGetAndHead(t *testing.T) {
	runtime := newTestRuntime(t)

	get := httptest.NewRecorder()
	runtime.handler.ServeHTTP(get, httptest.NewRequest(http.MethodGet, metricsPath, nil))
	if get.Code != http.StatusOK || !strings.Contains(get.Body.String(), "mindclade_control_admission_decisions_total") {
		t.Fatalf("GET /metrics = %d body=%q", get.Code, get.Body.String())
	}
	if get.Header().Get("Cache-Control") != "no-store" || get.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("GET security headers = %v", get.Header())
	}

	head := httptest.NewRecorder()
	runtime.handler.ServeHTTP(head, httptest.NewRequest(http.MethodHead, metricsPath, nil))
	if head.Code != http.StatusOK || head.Body.Len() != 0 {
		t.Fatalf("HEAD /metrics = %d body-bytes=%d", head.Code, head.Body.Len())
	}

	post := httptest.NewRecorder()
	runtime.handler.ServeHTTP(post, httptest.NewRequest(http.MethodPost, metricsPath, nil))
	if post.Code != http.StatusMethodNotAllowed || post.Header().Get("Allow") != "GET, HEAD" {
		t.Fatalf("POST /metrics = %d allow=%q", post.Code, post.Header().Get("Allow"))
	}

	wrong := httptest.NewRecorder()
	runtime.handler.ServeHTTP(wrong, httptest.NewRequest(http.MethodGet, metricsPath+"/extra", nil))
	if wrong.Code != http.StatusNotFound {
		t.Fatalf("GET wrong path = %d", wrong.Code)
	}
}

func TestMetricsLifecycleServesAndStops(t *testing.T) {
	runtime := newTestRuntime(t)
	component := runtime.Component()
	if err := component.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- component.Run(context.Background()) }()

	client := &http.Client{Transport: &http.Transport{Proxy: nil}, Timeout: time.Second}
	endpoint := "http://" + runtime.Address().String() + metricsPath
	deadline := time.Now().Add(2 * time.Second)
	for {
		response, err := client.Get(endpoint)
		if err == nil {
			_, _ = io.Copy(io.Discard, response.Body)
			_ = response.Body.Close()
			if response.StatusCode == http.StatusOK {
				break
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("metrics listener never became ready: %v", err)
		}
		time.Sleep(5 * time.Millisecond)
	}

	stopCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := component.Stop(stopCtx); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("metrics run returned %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("metrics run did not stop")
	}
	if _, err := client.Get(endpoint); err == nil {
		t.Fatal("metrics listener accepted traffic after stop")
	}
}

func TestMetricsListenerConflictFailsClosed(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	if _, err := New(listener.Addr().String(), time.Second); err == nil {
		t.Fatal("occupied metrics address was accepted")
	} else if faults.CodeOf(err) != faults.CodeUnavailable || faults.ReasonOf(err) != "metrics_listener_failed" {
		t.Fatalf("listener failure = code %s reason %q: %v", faults.CodeOf(err), faults.ReasonOf(err), err)
	}
}

func TestMetricsRejectsEmptyAddress(t *testing.T) {
	if _, err := New(" ", time.Second); err == nil {
		t.Fatal("empty metrics address was accepted")
	} else if faults.ReasonOf(err) != "admission_metrics_configuration_invalid" {
		t.Fatalf("reason = %q", faults.ReasonOf(err))
	}
}

type capabilityResponseWriter struct {
	header   http.Header
	flushed  bool
	hijacked bool
	pushed   string
	read     int64
}

type basicResponseWriter struct {
	header http.Header
}

func (writer *basicResponseWriter) Header() http.Header      { return writer.header }
func (*basicResponseWriter) WriteHeader(int)                 {}
func (*basicResponseWriter) Write(value []byte) (int, error) { return len(value), nil }

func (writer *capabilityResponseWriter) Header() http.Header      { return writer.header }
func (*capabilityResponseWriter) WriteHeader(int)                 {}
func (*capabilityResponseWriter) Write(value []byte) (int, error) { return len(value), nil }
func (writer *capabilityResponseWriter) Flush()                   { writer.flushed = true }
func (writer *capabilityResponseWriter) Push(target string, _ *http.PushOptions) error {
	writer.pushed = target
	return nil
}
func (writer *capabilityResponseWriter) ReadFrom(reader io.Reader) (int64, error) {
	read, err := io.Copy(io.Discard, reader)
	writer.read += read
	return read, err
}
func (writer *capabilityResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	writer.hijacked = true
	return nil, nil, errors.New("test hijack")
}

func newTestRuntime(t *testing.T) *Runtime {
	t.Helper()
	runtime, err := New("127.0.0.1:0", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := runtime.Close(); err != nil {
			t.Errorf("close metrics runtime: %v", err)
		}
	})
	return runtime
}

func scrape(t *testing.T, handler http.Handler, method, path string) string {
	t.Helper()
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(method, path, nil))
	if response.Code != http.StatusOK {
		t.Fatalf("scrape status = %d body=%q", response.Code, response.Body.String())
	}
	return response.Body.String()
}

func metricValue(t *testing.T, body, metricPattern string) float64 {
	t.Helper()
	match := regexp.MustCompile(metricPattern + ` ([0-9.eE+-]+)`).FindStringSubmatch(body)
	if len(match) != 2 {
		t.Fatalf("metric %s missing:\n%s", metricPattern, body)
	}
	value, err := strconv.ParseFloat(match[1], 64)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func testFault(code faults.Code) error {
	return faults.New(code, code.String(), faults.WithReason("test_"+code.String()))
}
