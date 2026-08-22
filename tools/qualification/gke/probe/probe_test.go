// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

package probe

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

type fakeRunner struct {
	outputs map[string][]byte
	errors  map[string]error
	calls   []string
}

func (runner *fakeRunner) Run(_ context.Context, name string, args ...string) ([]byte, error) {
	runner.calls = append(runner.calls, name+" "+strings.Join(args, " "))
	if err := runner.errors[name]; err != nil {
		return nil, err
	}
	return runner.outputs[name], nil
}

func scratch(t *testing.T) string {
	t.Helper()
	path, err := os.MkdirTemp("/tmp", "mindclade-qualification-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(path) })
	return path
}

func baseConfig(t *testing.T, profile string) Config {
	t.Helper()
	return Config{
		Profile:         profile,
		Scratch:         scratch(t),
		Contract:        ContractVersion,
		RunID:           "qualification-123",
		SourceRevision:  strings.Repeat("a", 40),
		Image:           "us-docker.pkg.dev/platform/qualification/probe@sha256:" + strings.Repeat("b", 64),
		RequireComplete: true,
		IOBytes:         128 * 1024,
		IPCBytes:        128 * 1024,
	}
}

func fixedClock() func() time.Time {
	values := []time.Time{
		time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC),
		time.Date(2026, 8, 20, 12, 0, 1, 0, time.UTC),
	}
	return func() time.Time {
		value := values[0]
		if len(values) > 1 {
			values = values[1:]
		}
		return value
	}
}

func TestCPUQualificationIsLocalAndComplete(t *testing.T) {
	runner := &fakeRunner{outputs: map[string][]byte{}, errors: map[string]error{}}
	result, err := Execute(context.Background(), baseConfig(t, "cpu"), Dependencies{
		Commands: runner,
		Now:      fixedClock(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Result != "pass" || result.SchemaVersion != ResultSchema || len(result.Checks) != 3 {
		t.Fatalf("unexpected result: %#v", result)
	}
	if len(runner.calls) != 0 {
		t.Fatalf("CPU qualification invoked external commands: %v", runner.calls)
	}
	for _, check := range result.Checks {
		if check.Status != "pass" {
			t.Fatalf("check did not pass: %#v", check)
		}
		for name, value := range check.Measurements {
			if value <= 0 {
				t.Fatalf("measurement %s is not positive: %v", name, value)
			}
		}
	}
}

func TestGPUQualificationRequiresInventoryAndStrictHelper(t *testing.T) {
	for _, profile := range []string{"h100", "b200"} {
		t.Run(profile, func(t *testing.T) {
			memoryMiB := 80 * 1024
			if profile == "b200" {
				memoryMiB = 180 * 1024
			}
			inventory := strings.Repeat(fmt.Sprintf("NVIDIA %s, %d\n", strings.ToUpper(profile), memoryMiB), 8)
			helper, err := json.Marshal(GPUHelperResult{
				SchemaVersion:        HelperSchema,
				Profile:              profile,
				GPUCount:             8,
				CUDADeviceCount:      8,
				NCCLAllReduceBytes:   1024 * 1024 * 1024,
				NCCLBusBandwidthGBPS: 100,
				GPUMemoryTestedBytes: 1024 * 1024 * 1024,
				DCGMHealth:           "pass",
			})
			if err != nil {
				t.Fatal(err)
			}
			runner := &fakeRunner{
				outputs: map[string][]byte{
					"nvidia-smi":  []byte(inventory),
					GPUHelperPath: helper,
				},
				errors: map[string]error{},
			}
			result, err := Execute(context.Background(), baseConfig(t, profile), Dependencies{
				Commands: runner,
				Now:      fixedClock(),
			})
			if err != nil {
				t.Fatal(err)
			}
			if result.Hardware.GPUCount != 8 || len(result.Checks) != 5 {
				t.Fatalf("unexpected GPU result: %#v", result)
			}
			if len(runner.calls) != 2 || !strings.HasPrefix(runner.calls[1], GPUHelperPath+" ") {
				t.Fatalf("unexpected command calls: %v", runner.calls)
			}
		})
	}
}

func TestGPUQualificationFailsClosedWithoutHelper(t *testing.T) {
	inventory := strings.Repeat("NVIDIA H100 80GB, 81920\n", 8)
	runner := &fakeRunner{
		outputs: map[string][]byte{"nvidia-smi": []byte(inventory)},
		errors:  map[string]error{GPUHelperPath: fmt.Errorf("not found")},
	}
	_, err := Execute(context.Background(), baseConfig(t, "h100"), Dependencies{
		Commands: runner,
		Now:      fixedClock(),
	})
	if err == nil || !strings.Contains(err.Error(), "CUDA/NCCL helper qualification failed") {
		t.Fatalf("expected fail-closed helper error, got %v", err)
	}
}

func TestGPUQualificationRejectsTrivialMemoryTest(t *testing.T) {
	inventory := strings.Repeat("NVIDIA H100 80GB, 81920\n", 8)
	helper, err := json.Marshal(GPUHelperResult{
		SchemaVersion:        HelperSchema,
		Profile:              "h100",
		GPUCount:             8,
		CUDADeviceCount:      8,
		NCCLAllReduceBytes:   1024 * 1024 * 1024,
		NCCLBusBandwidthGBPS: 100,
		GPUMemoryTestedBytes: 1,
		DCGMHealth:           "pass",
	})
	if err != nil {
		t.Fatal(err)
	}
	runner := &fakeRunner{
		outputs: map[string][]byte{
			"nvidia-smi":  []byte(inventory),
			GPUHelperPath: helper,
		},
		errors: map[string]error{},
	}
	_, err = Execute(context.Background(), baseConfig(t, "h100"), Dependencies{
		Commands: runner,
		Now:      fixedClock(),
	})
	if err == nil || !strings.Contains(err.Error(), "result is incomplete") {
		t.Fatalf("expected a trivial GPU memory check to fail, got %v", err)
	}
}

func TestQualificationRejectsMissingProvenanceAndZeroDigest(t *testing.T) {
	config := baseConfig(t, "cpu")
	config.SourceRevision = "main"
	if _, err := Execute(context.Background(), config, Dependencies{}); err == nil {
		t.Fatal("expected mutable source revision to fail")
	}
	config = baseConfig(t, "cpu")
	config.Image = "registry.invalid/probe@sha256:" + strings.Repeat("0", 64)
	if _, err := Execute(context.Background(), config, Dependencies{}); err == nil {
		t.Fatal("expected zero digest to fail")
	}
	config = baseConfig(t, "cpu")
	config.RequireComplete = false
	if _, err := Execute(context.Background(), config, Dependencies{}); err == nil {
		t.Fatal("expected partial mode to fail")
	}
}

func TestQualificationRejectsUnboundedWorkAndHonorsCancellation(t *testing.T) {
	config := baseConfig(t, "cpu")
	config.IOBytes = maximumIOBytes + 1
	if _, err := Execute(context.Background(), config, Dependencies{}); err == nil ||
		!strings.Contains(err.Error(), "storage qualification byte count is outside bounds") {
		t.Fatalf("expected storage bound failure, got %v", err)
	}

	config = baseConfig(t, "cpu")
	config.IPCBytes = maximumIPCBytes + 1
	if _, err := Execute(context.Background(), config, Dependencies{}); err == nil ||
		!strings.Contains(err.Error(), "IPC qualification byte count is outside bounds") {
		t.Fatalf("expected IPC bound failure, got %v", err)
	}

	config = baseConfig(t, "cpu")
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Execute(canceled, config, Dependencies{}); err == nil ||
		!strings.Contains(err.Error(), "canceled") {
		t.Fatalf("expected canceled qualification to fail, got %v", err)
	}

	if _, err := Execute(nil, baseConfig(t, "cpu"), Dependencies{}); err == nil ||
		!strings.Contains(err.Error(), "context is required") {
		t.Fatalf("expected nil context to fail, got %v", err)
	}
}
