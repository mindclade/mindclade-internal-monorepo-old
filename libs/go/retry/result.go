// Copyright 2026 Mindclade. All rights reserved.
// Confidential and proprietary.

package retry

import "time"

// Outcome is the terminal state of an execution.
type Outcome string

const (
	OutcomeSucceeded   Outcome = "succeeded"
	OutcomeStopped     Outcome = "stopped"
	OutcomeExhausted   Outcome = "exhausted"
	OutcomeInterrupted Outcome = "interrupted"
)

// Result is an immutable execution summary.
type Result struct {
	operation  string
	attempts   int
	startedAt  time.Time
	finishedAt time.Time
	lastDelay  time.Duration
	outcome    Outcome
	lastErr    error
}

func (result Result) Operation() string        { return result.operation }
func (result Result) Attempts() int            { return result.attempts }
func (result Result) StartedAt() time.Time     { return result.startedAt }
func (result Result) FinishedAt() time.Time    { return result.finishedAt }
func (result Result) LastDelay() time.Duration { return result.lastDelay }
func (result Result) Outcome() Outcome         { return result.outcome }
func (result Result) LastError() error         { return result.lastErr }

func (result Result) Elapsed() time.Duration {
	if result.startedAt.IsZero() || result.finishedAt.IsZero() || result.finishedAt.Before(result.startedAt) {
		return 0
	}
	return result.finishedAt.Sub(result.startedAt)
}

func (result Result) Succeeded() bool { return result.outcome == OutcomeSucceeded }
