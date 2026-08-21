// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package admissionmetrics

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"go.mindclade.dev/control/admission"
	"go.mindclade.dev/libs/go/faults"
	"go.mindclade.dev/libs/go/identifiers"
	"go.mindclade.dev/libs/go/resourceversion"
)

type testEngine struct {
	admit   func(context.Context, admission.AdmitRequest) (admission.Decision, error)
	commit  func(context.Context, identifiers.ID, resourceversion.Version, identifiers.Digest, string, admission.Quota) (admission.Decision, error)
	release func(context.Context, identifiers.ID, resourceversion.Version, identifiers.Digest, string) (admission.Decision, error)
}

func (engine *testEngine) Admit(ctx context.Context, request admission.AdmitRequest) (admission.Decision, error) {
	if engine.admit == nil {
		return admission.Decision{}, nil
	}
	return engine.admit(ctx, request)
}

func (engine *testEngine) Commit(ctx context.Context, id identifiers.ID, expected resourceversion.Version, digest identifiers.Digest, subject string, actual admission.Quota) (admission.Decision, error) {
	if engine.commit == nil {
		return admission.Decision{}, nil
	}
	return engine.commit(ctx, id, expected, digest, subject, actual)
}

func (engine *testEngine) Release(ctx context.Context, id identifiers.ID, expected resourceversion.Version, digest identifiers.Digest, subject string) (admission.Decision, error) {
	if engine.release == nil {
		return admission.Decision{}, nil
	}
	return engine.release(ctx, id, expected, digest, subject)
}

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
		{name: "canceled", err: context.Canceled, want: resultDeadline},
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

func TestWrapperPreservesResultsAndOmitsHighCardinalityData(t *testing.T) {
	sentinel := faults.New(faults.CodeConflict, "sensitive-cause-9f2d",
		faults.WithReason("sensitive-reason-82b1"),
		faults.WithField("workspace", "sensitive-workspace-d438"))
	want := admission.Decision{Replayed: true}
	delegate := &testEngine{admit: func(context.Context, admission.AdmitRequest) (admission.Decision, error) {
		return want, sentinel
	}}
	runtime := newTestRuntime(t, delegate)
	request := admission.AdmitRequest{
		Workspace: "sensitive-workspace-d438",
		Subject:   "sensitive-subject-30cc",
	}
	got, err := runtime.Admit(context.Background(), request)
	if !reflect.DeepEqual(got, want) || !errors.Is(err, sentinel) {
		t.Fatalf("wrapper changed result: decision=%+v err=%v", got, err)
	}

	body := scrape(t, runtime.handler, http.MethodGet, metricsPath)
	if !strings.Contains(body, `mindclade_control_admission_decisions_total{operation="admit",result="conflict"} 1`) {
		t.Fatalf("conflict decision was not recorded:\n%s", body)
	}
	for _, forbidden := range []string{
		"sensitive-cause-9f2d", "sensitive-reason-82b1", "sensitive-workspace-d438", "sensitive-subject-30cc",
		"tenant=", "workspace=", "subject=", "model=", "provider=", "route=", "reason=", "request=", "reservation=", "idempotency=",
	} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("metrics leaked forbidden cardinality or data %q:\n%s", forbidden, body)
		}
	}
}

func TestMetricSeriesInventoryIsExact(t *testing.T) {
	runtime := newTestRuntime(t, &testEngine{})
	body := scrape(t, runtime.handler, http.MethodGet, metricsPath)
	pattern := regexp.MustCompile(`^mindclade_control_admission_decisions_total\{operation="([a-z_]+)",result="([a-z_]+)"\} 0$`)
	seen := make(map[string]struct{})
	for _, line := range strings.Split(body, "\n") {
		if !strings.HasPrefix(line, "mindclade_control_admission_decisions_total{") {
			continue
		}
		match := pattern.FindStringSubmatch(line)
		if len(match) != 3 {
			t.Fatalf("unexpected decision series identity %q", line)
		}
		seen[match[1]+"/"+match[2]] = struct{}{}
	}
	if len(seen) != len(operations)*len(results) {
		t.Fatalf("decision series count = %d, want %d", len(seen), len(operations)*len(results))
	}
	for _, operation := range operations {
		for _, result := range results {
			if _, ok := seen[operation+"/"+result]; !ok {
				t.Fatalf("missing bounded decision series %s/%s", operation, result)
			}
		}
	}
}

func TestPanicsAreCountedInternalAndStillPropagate(t *testing.T) {
	runtime := newTestRuntime(t, &testEngine{admit: func(context.Context, admission.AdmitRequest) (admission.Decision, error) {
		panic("sentinel-panic")
	}})
	var recovered any
	func() {
		defer func() { recovered = recover() }()
		_, _ = runtime.Admit(context.Background(), admission.AdmitRequest{})
	}()
	if recovered != "sentinel-panic" {
		t.Fatalf("panic = %v, want sentinel", recovered)
	}
	body := scrape(t, runtime.handler, http.MethodGet, metricsPath)
	if !strings.Contains(body, `mindclade_control_admission_decisions_total{operation="admit",result="internal"} 1`) {
		t.Fatalf("panic was not counted as internal:\n%s", body)
	}
	if !strings.Contains(body, `mindclade_control_admission_decision_duration_seconds_count{operation="admit"} 1`) {
		t.Fatalf("panic duration was not observed:\n%s", body)
	}
}

func TestWrapperRecordsEveryOperationAndTrueDuration(t *testing.T) {
	delegate := &testEngine{admit: func(context.Context, admission.AdmitRequest) (admission.Decision, error) {
		time.Sleep(10 * time.Millisecond)
		return admission.Decision{}, nil
	}}
	runtime := newTestRuntime(t, delegate)
	if _, err := runtime.Admit(context.Background(), admission.AdmitRequest{}); err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.Commit(context.Background(), identifiers.ID{}, resourceversion.Version{}, identifiers.Digest{}, "subject", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.Release(context.Background(), identifiers.ID{}, resourceversion.Version{}, identifiers.Digest{}, "subject"); err != nil {
		t.Fatal(err)
	}

	body := scrape(t, runtime.handler, http.MethodGet, metricsPath)
	for _, operation := range operations {
		if !strings.Contains(body, fmt.Sprintf(`mindclade_control_admission_decisions_total{operation=%q,result="allow"} 1`, operation)) {
			t.Fatalf("allow result missing for %s:\n%s", operation, body)
		}
		if !strings.Contains(body, fmt.Sprintf(`mindclade_control_admission_decision_duration_seconds_count{operation=%q} 1`, operation)) {
			t.Fatalf("duration count missing for %s:\n%s", operation, body)
		}
	}
	match := regexp.MustCompile(`mindclade_control_admission_decision_duration_seconds_sum\{operation="admit"\} ([0-9.eE+-]+)`).FindStringSubmatch(body)
	if len(match) != 2 {
		t.Fatalf("admit duration sum missing:\n%s", body)
	}
	seconds, err := strconv.ParseFloat(match[1], 64)
	if err != nil || seconds < 0.005 {
		t.Fatalf("admit duration = %q, want observed elapsed time", match[1])
	}
}

func TestWrapperIsConcurrentSafe(t *testing.T) {
	runtime := newTestRuntime(t, &testEngine{})
	const calls = 64
	var group sync.WaitGroup
	for index := 0; index < calls; index++ {
		group.Add(3)
		go func() {
			defer group.Done()
			_, _ = runtime.Admit(context.Background(), admission.AdmitRequest{})
		}()
		go func() {
			defer group.Done()
			_, _ = runtime.Commit(context.Background(), identifiers.ID{}, resourceversion.Version{}, identifiers.Digest{}, "subject", nil)
		}()
		go func() {
			defer group.Done()
			_, _ = runtime.Release(context.Background(), identifiers.ID{}, resourceversion.Version{}, identifiers.Digest{}, "subject")
		}()
	}
	group.Wait()

	body := scrape(t, runtime.handler, http.MethodGet, metricsPath)
	for _, operation := range operations {
		want := fmt.Sprintf(`mindclade_control_admission_decisions_total{operation=%q,result="allow"} %d`, operation, calls)
		if !strings.Contains(body, want) {
			t.Fatalf("concurrent count missing %q:\n%s", want, body)
		}
	}
}

func TestMetricsHandlerAllowsOnlyExactGetAndHead(t *testing.T) {
	runtime := newTestRuntime(t, &testEngine{})

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
	runtime := newTestRuntime(t, &testEngine{})
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
	if _, err := New(listener.Addr().String(), time.Second, &testEngine{}); err == nil {
		t.Fatal("occupied metrics address was accepted")
	} else if faults.CodeOf(err) != faults.CodeUnavailable || faults.ReasonOf(err) != "metrics_listener_failed" {
		t.Fatalf("listener failure = code %s reason %q: %v", faults.CodeOf(err), faults.ReasonOf(err), err)
	}
}

func TestMetricsRejectsNilAndTypedNilEngines(t *testing.T) {
	var typedNil *testEngine
	for name, engine := range map[string]Engine{"nil": nil, "typed_nil": typedNil} {
		t.Run(name, func(t *testing.T) {
			if _, err := New("127.0.0.1:0", time.Second, engine); err == nil {
				t.Fatal("nil engine was accepted")
			} else if faults.ReasonOf(err) != "admission_metrics_configuration_invalid" {
				t.Fatalf("reason = %q", faults.ReasonOf(err))
			}
		})
	}
}

func newTestRuntime(t *testing.T, engine Engine) *Runtime {
	t.Helper()
	runtime, err := New("127.0.0.1:0", time.Second, engine)
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

func testFault(code faults.Code) error {
	return faults.New(code, code.String(), faults.WithReason("test_"+code.String()))
}
