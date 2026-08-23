// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package local

import (
	"context"
	"errors"
	"os/exec"
	"strings"
	"time"

	"go.mindclade.dev/control/orchestration"
)

// MaximumCapturedBytes bounds each of stdout and stderr per workload.
//
// The bound exists because the captured output lives in the launcher's own
// memory for the lifetime of the record, and a local workload is under no
// obligation to be quiet. A run that writes a gigabyte to stderr must lose the
// tail of its log, not the control plane.
const MaximumCapturedBytes = 64 * 1024

// DefaultWaitDelay bounds how long a killed process may keep the launcher's
// pipes open after its context is cancelled. Without it, a child that ignores
// SIGKILL propagation to a grandchild holds Wait open forever, and because this
// launcher waits inline that would wedge the calling worker rather than leaking
// a background goroutine.
const DefaultWaitDelay = 5 * time.Second

// Result is what one bounded local execution produced.
type Result struct {
	// ExitCode is the process exit status. It is meaningful only when Run
	// returned no error.
	ExitCode int
	Stdout   []byte
	Stderr   []byte
	// Truncated reports that at least one stream hit MaximumCapturedBytes. It
	// is recorded rather than inferred from length, because output that is
	// exactly at the bound is not the same as output that was cut.
	Truncated bool
}

// Command is one prepared, runnable local workload.
//
// Run blocks until the workload finishes. That is the whole design: packages
// under control/ start no goroutines, so a local launcher either waits inline
// or lies about having started something. Waiting inline is the honest option,
// and it is acceptable precisely because this adapter is for development and
// component tests rather than for fleet execution.
type Command interface {
	Run(ctx context.Context) (Result, error)
}

// CommandFactory prepares the command for one workload.
//
// It is an interface rather than a function on the launcher so tests can run
// the full contract without spawning a process. A conformance suite that had
// to fork real binaries would be testing the operating system.
type CommandFactory interface {
	NewCommand(ctx context.Context, envelope orchestration.WorkloadEnvelope) (Command, error)
}

// Program is the executable a workload resolves to.
type Program struct {
	Path string
	Args []string
	// Env is the complete environment. It is never merged with the launcher's
	// own environment: a development runner that inherited the operator's
	// shell would hand every workload whatever credentials happened to be
	// exported, which is exactly the leak the execution ticket exists to bound.
	Env []string
	Dir string
}

func (program Program) validate() error {
	if strings.TrimSpace(program.Path) == "" {
		return invalid("program_path_required", "resolved program path is required", nil)
	}
	for _, value := range program.Env {
		if !strings.Contains(value, "=") {
			return invalid("program_env_invalid", "program environment entries must be key=value", nil)
		}
	}
	return nil
}

// Resolver turns an envelope into the program that executes it. Resolution is
// policy -- which image, which entry point, which sandbox -- and policy does
// not belong in an adapter.
type Resolver interface {
	Resolve(orchestration.WorkloadEnvelope) (Program, error)
}

// ExecCommands is the production CommandFactory. It is the only place in this
// package that touches os/exec.
type ExecCommands struct {
	Resolver Resolver
	// CaptureLimit overrides MaximumCapturedBytes when positive.
	CaptureLimit int
	// WaitDelay overrides DefaultWaitDelay when positive.
	WaitDelay time.Duration
}

// NewCommand resolves the program and wraps it in a runnable command.
func (factory ExecCommands) NewCommand(ctx context.Context, envelope orchestration.WorkloadEnvelope) (Command, error) {
	if ctx == nil {
		return nil, invalid("context_nil", "context is required", nil)
	}
	if nilInterface(factory.Resolver) {
		return nil, unavailable("resolver_unavailable", "local program resolver is unavailable", nil)
	}
	program, err := factory.Resolver.Resolve(envelope)
	if err != nil {
		return nil, err
	}
	if err := program.validate(); err != nil {
		return nil, err
	}
	limit := factory.CaptureLimit
	if limit <= 0 {
		limit = MaximumCapturedBytes
	}
	delay := factory.WaitDelay
	if delay <= 0 {
		delay = DefaultWaitDelay
	}
	return &execCommand{program: program, limit: limit, waitDelay: delay}, nil
}

type execCommand struct {
	program   Program
	limit     int
	waitDelay time.Duration
}

// Run starts the process and waits for it inline.
//
// A non-zero exit is a Result, not an error: the workload ran and produced an
// outcome, and turning that into a Go error would make "the program said no"
// indistinguishable from "the program could not be started", which are
// different dispositions for the stage above.
func (command *execCommand) Run(ctx context.Context) (Result, error) {
	if ctx == nil {
		return Result{}, invalid("context_nil", "context is required", nil)
	}
	if err := ctx.Err(); err != nil {
		return Result{}, canceled(err)
	}
	process := exec.CommandContext(ctx, command.program.Path, command.program.Args...)
	process.Dir = command.program.Dir
	// A nil Env inherits the parent environment, so an empty slice is not the
	// same as leaving this unset. Assigning unconditionally is what makes the
	// empty case mean "no environment".
	process.Env = append([]string{}, command.program.Env...)
	process.WaitDelay = command.waitDelay
	stdout := &boundedWriter{limit: command.limit}
	stderr := &boundedWriter{limit: command.limit}
	process.Stdout = stdout
	process.Stderr = stderr

	if err := process.Start(); err != nil {
		return Result{}, unavailable("local_process_start_failed", "local workload process could not be started", err)
	}
	waitErr := process.Wait()
	result := Result{
		Stdout:    stdout.bytes(),
		Stderr:    stderr.bytes(),
		Truncated: stdout.truncated || stderr.truncated,
	}
	if waitErr == nil {
		return result, nil
	}
	var exit *exec.ExitError
	if errors.As(waitErr, &exit) {
		result.ExitCode = exit.ExitCode()
		return result, nil
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		return result, canceled(ctxErr)
	}
	return Result{}, unavailable("local_process_wait_failed", "local workload process could not be reaped", waitErr)
}

// boundedWriter keeps at most limit bytes and reports whether it dropped any.
//
// It always claims to have written everything. Reporting a short write would
// make io.Copy inside os/exec fail and kill a workload for being verbose,
// which is a far worse outcome than losing the tail of a log.
type boundedWriter struct {
	limit     int
	buffer    []byte
	truncated bool
}

func (writer *boundedWriter) Write(payload []byte) (int, error) {
	remaining := writer.limit - len(writer.buffer)
	switch {
	case remaining <= 0:
		writer.truncated = len(payload) > 0 || writer.truncated
	case len(payload) > remaining:
		writer.buffer = append(writer.buffer, payload[:remaining]...)
		writer.truncated = true
	default:
		writer.buffer = append(writer.buffer, payload...)
	}
	return len(payload), nil
}

func (writer *boundedWriter) bytes() []byte {
	if len(writer.buffer) == 0 {
		return nil
	}
	return append([]byte(nil), writer.buffer...)
}
