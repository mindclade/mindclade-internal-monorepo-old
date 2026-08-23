// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package webhook

import (
	"context"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	_ "github.com/lib/pq"

	foundationconfig "go.mindclade.dev/libs/go/config"
	"go.mindclade.dev/libs/go/coordination/workqueue"
	"go.mindclade.dev/libs/go/coordination/workqueue/memory"
	"go.mindclade.dev/libs/go/faults"
	"go.mindclade.dev/libs/go/requestmeta"
	"go.mindclade.dev/services/control_plane/internal/bootstrap"
	"go.mindclade.dev/services/control_plane/internal/config"
)

func webhookSettings() foundationconfig.MapSource {
	return foundationconfig.MapSource{SourceName: "test", Values: map[string]string{
		"signing.hmac_key":       "01234567890123456789012345678901",
		"database.dsn":           "postgres://control:control@127.0.0.1:5432/control?sslmode=require",
		"messaging.provider":     "memory",
		"messaging.topic":        "mindclade.control.events",
		"outbound.allowed_hosts": "hooks.example.com,events.example.net",
	}}
}

// Building through servicekit/production is the assertion that matters: the
// webhook dispatcher is the only role that requires CapabilityOutboundHTTP,
// and Build fails unless a worker occupies the work stage as well.
func TestWebhookFactoryBuildsThroughProductionLifecycle(t *testing.T) {
	profile, err := bootstrap.ProfileFor(bootstrap.RoleWebhookDispatcher)
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := NewWebhookFactory(webhookSettings()).Create(context.Background(), profile)
	if err != nil {
		t.Fatal(err)
	}
	service, err := bootstrap.Build(profile, runtime)
	if err != nil {
		t.Fatalf("webhook-dispatcher runtime does not satisfy its production role: %v", err)
	}
	if service == nil || service.Service() == nil {
		t.Fatal("production runtime was not assembled")
	}
}

// The dispatcher delivers. It serves no transport, reaches no cluster, holds
// no lease, and projects nothing. Composing an aggregate it does not need
// would put those packages back into its import graph.
func TestWebhookComposesOnlyWhatItsRoleNeeds(t *testing.T) {
	profile, err := bootstrap.ProfileFor(bootstrap.RoleWebhookDispatcher)
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := NewWebhookFactory(webhookSettings()).Create(context.Background(), profile)
	if err != nil {
		t.Fatal(err)
	}
	capabilities := bootstrap.Capabilities(runtime.Dependencies...)
	present := make(map[string]struct{}, len(capabilities))
	for _, capability := range capabilities {
		present[capability.String()] = struct{}{}
	}
	for _, absent := range []string{
		"http", "grpc", "connect", "authentication", "authorization",
		"kubernetes", "kubernetes_manager", "lease_store", "leadership",
		"projector", "inbox_processor", "cursor_store", "migrations",
		"blob_store", "cache", "outbox_dispatcher",
	} {
		if _, found := present[absent]; found {
			t.Fatalf("webhook-dispatcher composes %q, which its role does not require", absent)
		}
	}
}

// A dispatcher that will call any host it is handed is a server-side request
// forgery primitive with a queue in front of it.
func TestEgressRequiresAnAllowList(t *testing.T) {
	source := webhookSettings()
	source.Values["outbound.allowed_hosts"] = ""
	settings, err := decodeSettings(t, source)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := newEgressClient(settings); err == nil {
		t.Fatal("egress client accepted an empty allow-list")
	} else if reason := faults.ReasonOf(err); reason != "outbound_allowed_hosts_not_configured" {
		t.Fatalf("reason=%s", reason)
	}
}

// The egress policy is not configurable per deployment. Every relaxation is a
// way to reach something inside the fleet's own network, so the client refuses
// plaintext and private addresses regardless of settings.
func TestEgressPolicyRefusesPlaintextAndPrivateAddresses(t *testing.T) {
	settings, err := decodeSettings(t, webhookSettings())
	if err != nil {
		t.Fatal(err)
	}
	client, err := newEgressClient(settings)
	if err != nil {
		t.Fatal(err)
	}
	if client == nil {
		t.Fatal("egress client was not constructed")
	}
	request, err := http.NewRequest(http.MethodPost, "http://hooks.example.com/hook", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Do(request); err == nil {
		t.Fatal("egress client accepted a plaintext target")
	}
	blocked, err := http.NewRequest(http.MethodPost, "https://127.0.0.1/hook", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Do(blocked); err == nil {
		t.Fatal("egress client accepted a private address")
	}
}

// An unconfigured delivery policy must announce itself. A webhook that reports
// success without being sent is indistinguishable, to the receiver, from one
// that was never queued.
func TestUnconfiguredDeliveryFailsClosed(t *testing.T) {
	if _, err := refuseDelivery(context.Background(), workqueue.Item{}); err == nil {
		t.Fatal("default delivery handler returned no error")
	} else if reason := faults.ReasonOf(err); reason != "delivery_handler_not_configured" {
		t.Fatalf("reason=%s", reason)
	}
}

// An injected handler replaces the fail-closed default.
func TestInjectedDeliveryHandlerReplacesTheDefault(t *testing.T) {
	profile, err := bootstrap.ProfileFor(bootstrap.RoleWebhookDispatcher)
	if err != nil {
		t.Fatal(err)
	}
	handler := workqueue.HandlerFunc(func(context.Context, workqueue.Item) (workqueue.Result, error) {
		return workqueue.Result{}, nil
	})
	factory := NewWebhookFactory(webhookSettings()).WithDeliveryHandler(handler)
	runtime, err := factory.Create(context.Background(), profile)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := bootstrap.Build(profile, runtime); err != nil {
		t.Fatalf("injected delivery handler does not satisfy the role: %v", err)
	}
}

func decodeSettings(t *testing.T, source foundationconfig.MapSource) (config.Settings, error) {
	t.Helper()
	resolved, err := config.Load(context.Background(), "control-plane-webhook-dispatcher", source)
	if err != nil {
		return config.Settings{}, err
	}
	return resolved.Settings, nil
}

// TestUnconfiguredDeliveryDeadLettersWithoutSpendingTheAttemptBudget pins the
// mechanism behind the fail-closed default, not just its error code.
//
// refuseDelivery carries faults.NoRetry(), and the worker treats a
// non-retryable handler error as terminal on the spot
// (terminal := !faults.IsRetryable(handlerErr) || attempts >= MaxAttempts). So
// an unconfigured dispatcher buries the item on its first attempt instead of
// re-leasing it until the budget runs out.
//
// That distinction is the point. A delivery attempt is an outbound request to
// a third party, so retrying a delivery policy that is *missing* would spend
// the item's whole budget re-entering a path that cannot succeed. The item has
// to be visible as dead-lettered immediately, with its remaining attempts
// unspent, because an operator reading a queue depth is how anyone finds out
// the dispatcher has no delivery policy at all.
func TestUnconfiguredDeliveryDeadLettersWithoutSpendingTheAttemptBudget(t *testing.T) {
	store := memory.New()
	// A deliberately generous budget: were the default handler ever made
	// retryable, the item would burn all five attempts before being buried and
	// this test would observe attempts > 1.
	item, err := workqueue.NewItem(
		deliveryQueue,
		[]byte(`{"endpoint":"https://hooks.example.com/hook"}`),
		0, time.Time{}, 5, requestmeta.Metadata{},
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Enqueue(context.Background(), item); err != nil {
		t.Fatal(err)
	}

	var calls atomic.Int32
	worker, err := workqueue.NewWorker(
		store,
		workqueue.HandlerFunc(func(ctx context.Context, claimed workqueue.Item) (workqueue.Result, error) {
			calls.Add(1)
			return refuseDelivery(ctx, claimed)
		}),
		workqueue.WorkerConfig{
			Owner:             "webhook-dispatcher-test",
			Queues:            []string{deliveryQueue},
			PollInterval:      time.Millisecond,
			LeaseDuration:     time.Second,
			HeartbeatInterval: 100 * time.Millisecond,
			BatchSize:         1,
			Concurrency:       1,
			// Short enough that a retrying handler would visibly spend its
			// budget inside this test's deadline rather than time out.
			FailureDelay: time.Millisecond,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- worker.Run(ctx) }()
	defer func() {
		cancel()
		<-done
	}()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		record, lookupErr := store.Lookup(context.Background(), item.ID)
		if lookupErr != nil {
			t.Fatal(lookupErr)
		}
		if record.State == workqueue.StateFailed {
			if record.Attempts != 1 {
				t.Fatalf("unconfigured delivery spent %d attempts before dead-lettering, want 1: "+
					"a missing delivery policy must not be retried against a third-party endpoint", record.Attempts)
			}
			if invocations := calls.Load(); invocations != 1 {
				t.Fatalf("delivery handler ran %d times, want 1", invocations)
			}
			if record.LastError == "" {
				t.Fatal("dead-lettered webhook kept no failure reason")
			}
			return
		}
		time.Sleep(time.Millisecond)
	}
	record, _ := store.Lookup(context.Background(), item.ID)
	t.Fatalf("unconfigured webhook delivery was never dead-lettered: state=%q attempts=%d handler_calls=%d",
		record.State, record.Attempts, calls.Load())
}
