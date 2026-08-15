// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package controlplaneapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	controlplaneapi "go.mindclade.dev/examples/go/control_plane_api"
)

func TestCreateRunPublishesEventAndAudit(t *testing.T) {
	app, err := controlplaneapi.New(controlplaneapi.Config{})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- app.Run(ctx) }()
	waitReady(t, app.Address())

	body := bytes.NewBufferString(`{"name":"novafold-evaluation"}`)
	response, err := http.Post("http://"+app.Address()+"/v1/runs", "application/json", body)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusCreated {
		cancel()
		t.Fatalf("status=%d", response.StatusCode)
	}
	var run controlplaneapi.Run
	if err := json.NewDecoder(response.Body).Decode(&run); err != nil {
		cancel()
		t.Fatal(err)
	}
	if run.ID == "" || run.Name != "novafold-evaluation" {
		cancel()
		t.Fatalf("run=%+v", run)
	}

	eventCtx, eventCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer eventCancel()
	event, err := app.NextEvent(eventCtx)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	if event.Topic() != "runs.created" || event.OrderingKey() != run.ID {
		cancel()
		t.Fatalf("event topic=%q ordering=%q", event.Topic(), event.OrderingKey())
	}
	if len(app.AuditEvents()) != 1 {
		cancel()
		t.Fatalf("audit events=%d", len(app.AuditEvents()))
	}

	getResponse, err := http.Get("http://" + app.Address() + "/v1/runs/" + run.ID)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	_ = getResponse.Body.Close()
	if getResponse.StatusCode != http.StatusOK {
		cancel()
		t.Fatalf("get status=%d", getResponse.StatusCode)
	}

	cancel()
	select {
	case err := <-done:
		if !controlplaneapi.IsExpectedShutdown(err) {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("service did not drain")
	}
}

func waitReady(t *testing.T, address string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		response, err := http.Get("http://" + address + "/readyz")
		if err == nil {
			_ = response.Body.Close()
			if response.StatusCode == http.StatusOK {
				return
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("service did not become ready")
}
