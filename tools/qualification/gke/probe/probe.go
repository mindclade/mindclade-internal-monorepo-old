// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

// Package probe implements the local, fail-closed portion of GKE foundation qualification.
package probe

import (
	"bytes"
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"
)

const (
	ContractVersion  = "gke-foundation-v1"
	ResultSchema     = "mindclade.dev/gke-foundation-probe-result/v1"
	HelperSchema     = "mindclade.dev/gpu-qualification-helper-result/v1"
	GPUHelperPath    = "/opt/mindclade/bin/release-gpu-qualification"
	GKEVersion       = "1.36.2-gke.2064000"
	expectedGPUCount = 8
	defaultIOBytes   = 8 * 1024 * 1024
	defaultIPCBytes  = 8 * 1024 * 1024
)

//go:embed contract.json
var embeddedContract []byte

type contractDocument struct {
	ContractVersion       string `json:"contract_version"`
	GKEVersion            string `json:"gke_version"`
	GPUHelperSchema       string `json:"gpu_helper_schema"`
	ProbeSchema           string `json:"probe_schema"`
	QualificationContract string `json:"qualification_contract"`
	RequestSchema         string `json:"request_schema"`
	ResultSchema          string `json:"result_schema"`
	SourceRepository      string `json:"source_repository"`
	TargetID              string `json:"target_id"`
}

func init() {
	var document contractDocument
	decoder := json.NewDecoder(bytes.NewReader(embeddedContract))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&document); err != nil {
		panic(fmt.Sprintf("invalid embedded qualification contract: %v", err))
	}
	if decoder.Decode(&struct{}{}) != io.EOF {
		panic("embedded qualification contract contains more than one JSON value")
	}
	if document.ContractVersion != "1.0.0" ||
		document.GKEVersion != GKEVersion ||
		document.GPUHelperSchema != HelperSchema ||
		document.ProbeSchema != ResultSchema ||
		document.QualificationContract != ContractVersion ||
		document.RequestSchema != "mindclade.dev/gke-foundation-qualification-request/v1" ||
		document.ResultSchema != "mindclade.dev/gke-foundation-qualification-result/v1" ||
		document.SourceRepository != "mindclade/mindclade-internal-monorepo" ||
		document.TargetID != "foundation-gke-qualification" {
		panic("embedded qualification contract does not match probe constants")
	}
}

var (
	runIDPattern    = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,38}[a-z0-9])?$`)
	revisionPattern = regexp.MustCompile(`^[0-9a-f]{40}$`)
	imagePattern    = regexp.MustCompile(`^[a-z0-9][a-z0-9._/:~-]*@sha256:[0-9a-f]{64}$`)
	zeroDigest      = "@sha256:" + strings.Repeat("0", 64)
)

// Config is the provenance-bearing execution contract supplied by the connected qualification
// overlay. No value is inferred from the cluster or a mutable tag.
type Config struct {
	Profile         string
	Scratch         string
	Contract        string
	RunID           string
	SourceRevision  string
	Image           string
	RequireComplete bool
	IOBytes         int64
	IPCBytes        int64
}

// CommandRunner makes external GPU inspection injectable and testable. The release image does
// not pretend that a successful nvidia-smi call proves CUDA or NCCL behavior.
type CommandRunner interface {
	Run(context.Context, string, ...string) ([]byte, error)
}

type ExecRunner struct{}

func (ExecRunner) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	command := exec.CommandContext(ctx, name, args...)
	output, err := command.Output()
	if err != nil {
		var exitError *exec.ExitError
		if errors.As(err, &exitError) {
			return nil, fmt.Errorf("%s failed: %s", name, strings.TrimSpace(string(exitError.Stderr)))
		}
		return nil, fmt.Errorf("%s failed: %w", name, err)
	}
	return output, nil
}

type Dependencies struct {
	Commands CommandRunner
	Now      func() time.Time
}

type Check struct {
	Name         string             `json:"name"`
	Status       string             `json:"status"`
	Measurements map[string]float64 `json:"measurements,omitempty"`
}

type Hardware struct {
	GPUName            string `json:"gpu_name,omitempty"`
	GPUCount           int    `json:"gpu_count,omitempty"`
	MinimumMemoryBytes uint64 `json:"minimum_gpu_memory_bytes,omitempty"`
}

type Result struct {
	SchemaVersion  string    `json:"schema_version"`
	Contract       string    `json:"contract"`
	Profile        string    `json:"profile"`
	RunID          string    `json:"run_id"`
	SourceRevision string    `json:"source_revision"`
	Image          string    `json:"image"`
	StartedAt      time.Time `json:"started_at"`
	CompletedAt    time.Time `json:"completed_at"`
	Result         string    `json:"result"`
	Hardware       Hardware  `json:"hardware"`
	Checks         []Check   `json:"checks"`
}

type GPUHelperResult struct {
	SchemaVersion        string  `json:"schema_version"`
	Profile              string  `json:"profile"`
	GPUCount             int     `json:"gpu_count"`
	CUDADeviceCount      int     `json:"cuda_device_count"`
	NCCLAllReduceBytes   uint64  `json:"nccl_all_reduce_bytes"`
	NCCLBusBandwidthGBPS float64 `json:"nccl_bus_bandwidth_gbps"`
	GPUMemoryTestedBytes uint64  `json:"gpu_memory_tested_bytes"`
	DCGMHealth           string  `json:"dcgm_health"`
}

func validateConfig(config Config) error {
	if config.Contract != ContractVersion {
		return fmt.Errorf("contract must be %q", ContractVersion)
	}
	if config.Profile != "cpu" && config.Profile != "h100" && config.Profile != "b200" {
		return fmt.Errorf("profile must be cpu, h100, or b200")
	}
	if !config.RequireComplete {
		return fmt.Errorf("--require-complete is mandatory")
	}
	if !runIDPattern.MatchString(config.RunID) {
		return fmt.Errorf("run id is not a bounded DNS label")
	}
	if !revisionPattern.MatchString(config.SourceRevision) {
		return fmt.Errorf("source revision must be exactly 40 lowercase hexadecimal characters")
	}
	if !imagePattern.MatchString(config.Image) || strings.HasSuffix(config.Image, zeroDigest) {
		return fmt.Errorf("image must use a nonzero sha256 digest")
	}
	if !filepath.IsAbs(config.Scratch) || filepath.Clean(config.Scratch) != config.Scratch {
		return fmt.Errorf("scratch must be an absolute normalized path")
	}
	info, err := os.Stat(config.Scratch)
	if err != nil {
		return fmt.Errorf("scratch is unavailable: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("scratch is not a directory")
	}
	return nil
}

func Execute(ctx context.Context, config Config, dependencies Dependencies) (Result, error) {
	if err := validateConfig(config); err != nil {
		return Result{}, err
	}
	if dependencies.Commands == nil {
		dependencies.Commands = ExecRunner{}
	}
	if dependencies.Now == nil {
		dependencies.Now = time.Now
	}
	if config.IOBytes == 0 {
		config.IOBytes = defaultIOBytes
	}
	if config.IPCBytes == 0 {
		config.IPCBytes = defaultIPCBytes
	}
	if config.IOBytes < 4096 || config.IPCBytes < 4096 {
		return Result{}, fmt.Errorf("qualification byte counts must each be at least 4096")
	}

	result := Result{
		SchemaVersion:  ResultSchema,
		Contract:       config.Contract,
		Profile:        config.Profile,
		RunID:          config.RunID,
		SourceRevision: config.SourceRevision,
		Image:          config.Image,
		StartedAt:      dependencies.Now().UTC().Truncate(time.Second),
		Result:         "pass",
	}

	storage, err := probeStorage(config.Scratch, config.IOBytes)
	if err != nil {
		return Result{}, fmt.Errorf("local storage qualification failed: %w", err)
	}
	result.Checks = append(result.Checks, storage)
	ipc, err := probeUnixIPC(config.Scratch, config.IPCBytes)
	if err != nil {
		return Result{}, fmt.Errorf("Unix IPC qualification failed: %w", err)
	}
	result.Checks = append(result.Checks, ipc)
	result.Checks = append(result.Checks, Check{
		Name:   "cpu-runtime",
		Status: "pass",
		Measurements: map[string]float64{
			"logical_cpu_count": float64(runtime.NumCPU()),
		},
	})

	if config.Profile != "cpu" {
		hardware, checks, err := probeGPU(ctx, config, dependencies.Commands)
		if err != nil {
			return Result{}, err
		}
		result.Hardware = hardware
		result.Checks = append(result.Checks, checks...)
	}
	result.CompletedAt = dependencies.Now().UTC().Truncate(time.Second)
	if result.CompletedAt.Before(result.StartedAt) {
		return Result{}, fmt.Errorf("qualification clock moved backwards")
	}
	return result, nil
}

func probeStorage(scratch string, byteCount int64) (Check, error) {
	file, err := os.CreateTemp(scratch, "mindclade-storage-*")
	if err != nil {
		return Check{}, err
	}
	name := file.Name()
	defer os.Remove(name)
	pattern := bytes.Repeat([]byte("mindclade-foundation-qualification\n"), 4096)
	writtenHash := sha256.New()
	started := time.Now()
	remaining := byteCount
	for remaining > 0 {
		chunk := pattern
		if int64(len(chunk)) > remaining {
			chunk = chunk[:remaining]
		}
		if _, err := file.Write(chunk); err != nil {
			file.Close()
			return Check{}, err
		}
		if _, err := writtenHash.Write(chunk); err != nil {
			file.Close()
			return Check{}, err
		}
		remaining -= int64(len(chunk))
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return Check{}, err
	}
	writeDuration := time.Since(started)
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		file.Close()
		return Check{}, err
	}
	readHash := sha256.New()
	readStarted := time.Now()
	read, err := io.Copy(readHash, file)
	closeErr := file.Close()
	if err != nil {
		return Check{}, err
	}
	if closeErr != nil {
		return Check{}, closeErr
	}
	if read != byteCount || !bytes.Equal(writtenHash.Sum(nil), readHash.Sum(nil)) {
		return Check{}, fmt.Errorf("write/read content digest mismatch")
	}
	readDuration := time.Since(readStarted)
	return Check{
		Name:   "local-storage-integrity",
		Status: "pass",
		Measurements: map[string]float64{
			"bytes_verified":       float64(byteCount),
			"read_mib_per_second":  rateMiB(byteCount, readDuration),
			"write_mib_per_second": rateMiB(byteCount, writeDuration),
		},
	}, nil
}

func probeUnixIPC(scratch string, byteCount int64) (Check, error) {
	path := filepath.Join(scratch, fmt.Sprintf("mcq-%d.sock", os.Getpid()))
	if len(path) > 100 {
		return Check{}, fmt.Errorf("Unix socket path exceeds portable length: %s", path)
	}
	os.Remove(path)
	listener, err := net.Listen("unix", path)
	if err != nil {
		return Check{}, err
	}
	defer os.Remove(path)
	defer listener.Close()
	serverResult := make(chan error, 1)
	go func() {
		connection, acceptErr := listener.Accept()
		if acceptErr != nil {
			serverResult <- acceptErr
			return
		}
		defer connection.Close()
		copied, copyErr := io.Copy(io.Discard, connection)
		if copyErr == nil && copied != byteCount {
			copyErr = fmt.Errorf("received %d bytes, expected %d", copied, byteCount)
		}
		serverResult <- copyErr
	}()
	connection, err := net.Dial("unix", path)
	if err != nil {
		return Check{}, err
	}
	started := time.Now()
	written, err := io.CopyN(connection, bytes.NewReader(bytes.Repeat([]byte{0xa5}, int(byteCount))), byteCount)
	if closeErr := connection.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return Check{}, err
	}
	if written != byteCount {
		return Check{}, fmt.Errorf("sent %d bytes, expected %d", written, byteCount)
	}
	if err := <-serverResult; err != nil {
		return Check{}, err
	}
	duration := time.Since(started)
	return Check{
		Name:   "unix-ipc",
		Status: "pass",
		Measurements: map[string]float64{
			"bytes_verified": float64(byteCount),
			"mib_per_second": rateMiB(byteCount, duration),
		},
	}, nil
}

func rateMiB(byteCount int64, duration time.Duration) float64 {
	seconds := duration.Seconds()
	if seconds <= 0 {
		seconds = 1e-9
	}
	return float64(byteCount) / (1024 * 1024) / seconds
}

func probeGPU(ctx context.Context, config Config, runner CommandRunner) (Hardware, []Check, error) {
	output, err := runner.Run(ctx, "nvidia-smi", "--query-gpu=name,memory.total", "--format=csv,noheader,nounits")
	if err != nil {
		return Hardware{}, nil, fmt.Errorf("GPU inventory qualification failed: %w", err)
	}
	records, err := csv.NewReader(bytes.NewReader(output)).ReadAll()
	if err != nil {
		return Hardware{}, nil, fmt.Errorf("GPU inventory is not valid CSV: %w", err)
	}
	if len(records) != expectedGPUCount {
		return Hardware{}, nil, fmt.Errorf("GPU inventory reports %d devices, expected %d", len(records), expectedGPUCount)
	}
	expectedName := strings.ToUpper(config.Profile)
	minimumMemoryMiB := uint64(75 * 1024)
	if config.Profile == "b200" {
		minimumMemoryMiB = 170 * 1024
	}
	minimumMemoryBytes := uint64(^uint64(0))
	canonicalName := ""
	for index, record := range records {
		if len(record) != 2 {
			return Hardware{}, nil, fmt.Errorf("GPU inventory row %d has %d fields", index, len(record))
		}
		name := strings.TrimSpace(record[0])
		if !strings.Contains(strings.ToUpper(name), expectedName) {
			return Hardware{}, nil, fmt.Errorf("GPU inventory row %d reports %q, expected %s", index, name, expectedName)
		}
		memoryMiB, parseErr := strconv.ParseUint(strings.TrimSpace(record[1]), 10, 64)
		if parseErr != nil || memoryMiB < minimumMemoryMiB {
			return Hardware{}, nil, fmt.Errorf("GPU inventory row %d has invalid memory", index)
		}
		memoryBytes := memoryMiB * 1024 * 1024
		if memoryBytes < minimumMemoryBytes {
			minimumMemoryBytes = memoryBytes
		}
		if canonicalName == "" {
			canonicalName = name
		} else if canonicalName != name {
			return Hardware{}, nil, fmt.Errorf("GPU inventory contains mixed models")
		}
	}

	helperOutput, err := runner.Run(
		ctx,
		GPUHelperPath,
		"--profile", config.Profile,
		"--expected-gpus", strconv.Itoa(expectedGPUCount),
		"--scratch", config.Scratch,
		"--format", "json",
	)
	if err != nil {
		return Hardware{}, nil, fmt.Errorf("CUDA/NCCL helper qualification failed: %w", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(helperOutput))
	decoder.DisallowUnknownFields()
	var helper GPUHelperResult
	if err := decoder.Decode(&helper); err != nil {
		return Hardware{}, nil, fmt.Errorf("CUDA/NCCL helper emitted invalid JSON: %w", err)
	}
	if decoder.Decode(&struct{}{}) != io.EOF {
		return Hardware{}, nil, fmt.Errorf("CUDA/NCCL helper emitted more than one JSON value")
	}
	if helper.SchemaVersion != HelperSchema || helper.Profile != config.Profile ||
		helper.GPUCount != expectedGPUCount || helper.CUDADeviceCount != expectedGPUCount ||
		helper.NCCLAllReduceBytes < 1024*1024*1024 || helper.NCCLBusBandwidthGBPS <= 0 ||
		helper.GPUMemoryTestedBytes < 1024*1024*1024 || helper.DCGMHealth != "pass" {
		return Hardware{}, nil, fmt.Errorf("CUDA/NCCL helper result is incomplete or inconsistent")
	}

	return Hardware{
			GPUName:            canonicalName,
			GPUCount:           expectedGPUCount,
			MinimumMemoryBytes: minimumMemoryBytes,
		}, []Check{
			{
				Name:   "gpu-inventory",
				Status: "pass",
				Measurements: map[string]float64{
					"gpu_count":                expectedGPUCount,
					"minimum_gpu_memory_bytes": float64(minimumMemoryBytes),
				},
			},
			{
				Name:   "cuda-nccl-dcgm",
				Status: "pass",
				Measurements: map[string]float64{
					"cuda_device_count":       float64(helper.CUDADeviceCount),
					"gpu_memory_tested_bytes": float64(helper.GPUMemoryTestedBytes),
					"nccl_all_reduce_bytes":   float64(helper.NCCLAllReduceBytes),
					"nccl_bus_bandwidth_gbps": helper.NCCLBusBandwidthGBPS,
				},
			},
		}, nil
}
