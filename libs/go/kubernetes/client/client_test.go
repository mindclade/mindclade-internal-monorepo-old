// Copyright 2026 Mindclade. All rights reserved.
// Confidential and proprietary.

package client

import (
	"context"
	"errors"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/version"
	"k8s.io/client-go/rest"
	crclient "sigs.k8s.io/controller-runtime/pkg/client"

	"mindclade.internal/libs/go/faults"
)

type versionDiscovery struct {
	info *version.Info
	err  error
}

func (discovery versionDiscovery) ServerVersion() (*version.Info, error) {
	return discovery.info, discovery.err
}

func TestConstructors(t *testing.T) {
	ctx := context.Background()
	config := &rest.Config{Host: "https://cluster.example", UserAgent: "original"}

	controllerClient, err := New(ctx, config, crclient.Options{})
	if err != nil || isNil(controllerClient) {
		t.Fatalf("New() = (%T, %v)", controllerClient, err)
	}
	typedClient, err := NewTyped(ctx, config)
	if err != nil || isNil(typedClient) {
		t.Fatalf("NewTyped() = (%T, %v)", typedClient, err)
	}
	dynamicClient, err := NewDynamic(ctx, config)
	if err != nil || isNil(dynamicClient) {
		t.Fatalf("NewDynamic() = (%T, %v)", dynamicClient, err)
	}
	discoveryClient, err := NewDiscovery(ctx, config)
	if err != nil || isNil(discoveryClient) {
		t.Fatalf("NewDiscovery() = (%T, %v)", discoveryClient, err)
	}
	if config.UserAgent != "original" {
		t.Fatal("constructor mutated caller-owned REST configuration")
	}
}

func TestConstructorsRejectInvalidInputs(t *testing.T) {
	config := &rest.Config{}
	tests := []struct {
		name string
		call func() error
	}{
		{"controller nil context", func() error { _, err := New(nil, config, crclient.Options{}); return err }},
		{"controller nil config", func() error { _, err := New(context.Background(), nil, crclient.Options{}); return err }},
		{"typed nil context", func() error { _, err := NewTyped(nil, config); return err }},
		{"typed nil config", func() error { _, err := NewTyped(context.Background(), nil); return err }},
		{"dynamic nil context", func() error { _, err := NewDynamic(nil, config); return err }},
		{"dynamic nil config", func() error { _, err := NewDynamic(context.Background(), nil); return err }},
		{"discovery nil context", func() error { _, err := NewDiscovery(nil, config); return err }},
		{"discovery nil config", func() error { _, err := NewDiscovery(context.Background(), nil); return err }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := test.call(); !faults.IsCode(err, faults.CodeInvalidArgument) {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestProbe(t *testing.T) {
	info, err := Probe(context.Background(), versionDiscovery{info: &version.Info{GitVersion: "v1.35.0"}})
	if err != nil || info.GitVersion != "v1.35.0" {
		t.Fatalf("Probe() = (%#v, %v)", info, err)
	}

	cause := errors.New("discovery unavailable")
	if _, err := Probe(context.Background(), versionDiscovery{err: cause}); !faults.IsCode(err, faults.CodeInternal) || !errors.Is(err, cause) {
		t.Fatalf("provider error = %v", err)
	}
	if _, err := Probe(context.Background(), versionDiscovery{}); !faults.IsCode(err, faults.CodeDataLoss) {
		t.Fatalf("nil version = %v", err)
	}
	if _, err := Probe(context.Background(), nil); !faults.IsCode(err, faults.CodeInvalidArgument) {
		t.Fatalf("nil client = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Probe(ctx, versionDiscovery{info: &version.Info{}}); !faults.IsCode(err, faults.CodeCanceled) {
		t.Fatalf("canceled context = %v", err)
	}
}

func TestLoadKubeconfigAndOverrides(t *testing.T) {
	config := Config{
		Source:         SourceKubeconfig,
		KubeconfigPath: "/tmp/kubeconfig",
		Context:        "production",
		Cluster:        "cluster-a",
		User:           "operator",
		UserAgent:      "mindclade-test",
		QPS:            50,
		Burst:          75,
		Timeout:        3 * time.Second,
	}
	loaded, err := Load(context.Background(), config)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.UserAgent != config.UserAgent || loaded.QPS != config.QPS || loaded.Burst != config.Burst || loaded.Timeout != config.Timeout {
		t.Fatalf("loaded config = %#v", loaded)
	}
	if got := config.String(); got != `source=kubeconfig context="production" cluster="cluster-a"` {
		t.Fatalf("String() = %q", got)
	}
	if SourceAuto.String() != "auto" {
		t.Fatalf("Source.String() = %q", SourceAuto.String())
	}
}

func TestLoadFailureAndCancellation(t *testing.T) {
	if _, err := Load(context.Background(), Config{Source: SourceAuto}); !faults.IsCode(err, faults.CodeFailedPrecondition) {
		t.Fatalf("auto unavailable = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Load(ctx, Config{Source: SourceKubeconfig, KubeconfigPath: "/tmp/kubeconfig"}); !faults.IsCode(err, faults.CodeCanceled) {
		t.Fatalf("canceled load = %v", err)
	}
	if _, err := Load(nil, Config{}); !faults.IsCode(err, faults.CodeInvalidArgument) {
		t.Fatalf("nil context = %v", err)
	}
}

func TestIsNil(t *testing.T) {
	var pointer *rest.Config
	if !isNil(pointer) || !isNil(nil) || isNil(rest.Config{}) {
		t.Fatal("isNil returned an unexpected result")
	}
}
