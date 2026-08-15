// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package clock

import (
	"container/heap"
	"context"
	"fmt"
	"sync"
	"time"
)

// FakeClock is a deterministic, manually advanced clock for tests.
//
// Advancing time delivers due timer values and at most one buffered value per
// ticker. When a large jump crosses several ticker intervals, intermediate
// ticks may be dropped, matching the standard library's permission to coalesce
// ticks for a slow receiver.
type FakeClock struct {
	mu      sync.Mutex
	now     time.Time
	nextID  uint64
	entries scheduleHeap
	changed chan struct{}
}

var _ Clock = (*FakeClock)(nil)

// NewFake constructs a fake clock at start. The monotonic component is removed
// so comparisons and serialized test values remain deterministic.
func NewFake(start time.Time) *FakeClock {
	clock := &FakeClock{
		now:     start.Round(0),
		changed: make(chan struct{}),
	}
	heap.Init(&clock.entries)
	return clock
}

// Now returns the fake clock's current time.
func (clock *FakeClock) Now() time.Time {
	if clock == nil {
		return time.Time{}
	}

	clock.mu.Lock()
	defer clock.mu.Unlock()
	return clock.now
}

// Since returns the elapsed fake duration since timestamp.
func (clock *FakeClock) Since(timestamp time.Time) time.Duration {
	return clock.Now().Sub(timestamp)
}

// Until returns the fake duration until timestamp.
func (clock *FakeClock) Until(timestamp time.Time) time.Duration {
	return timestamp.Sub(clock.Now())
}

// After returns a channel that receives once after duration.
func (clock *FakeClock) After(duration time.Duration) <-chan time.Time {
	return clock.NewTimer(duration).C()
}

// NewTimer creates a deterministic one-shot timer.
func (clock *FakeClock) NewTimer(duration time.Duration) Timer {
	if clock == nil {
		panic("clock: nil FakeClock")
	}

	clock.mu.Lock()
	defer clock.mu.Unlock()

	entry := clock.newEntryLocked(scheduleTimer)
	clock.configureTimerLocked(entry, duration)
	return &fakeTimer{clock: clock, entry: entry}
}

// NewTicker creates a deterministic ticker. It panics for non-positive
// durations, matching time.NewTicker.
func (clock *FakeClock) NewTicker(duration time.Duration) Ticker {
	if clock == nil {
		panic("clock: nil FakeClock")
	}
	if duration <= 0 {
		panic("clock: non-positive interval for NewTicker")
	}

	clock.mu.Lock()
	defer clock.mu.Unlock()

	entry := clock.newEntryLocked(scheduleTicker)
	clock.configureTickerLocked(entry, duration)
	return &fakeTicker{clock: clock, entry: entry}
}

// Sleep blocks until duration of fake time elapses or ctx is canceled.
func (clock *FakeClock) Sleep(ctx context.Context, duration time.Duration) error {
	return sleep(ctx, clock, duration)
}

// Advance moves the clock forward by duration and delivers all timers that
// become due. A negative duration returns ErrTimeReversal and leaves the clock
// unchanged.
func (clock *FakeClock) Advance(duration time.Duration) error {
	if clock == nil {
		return fmt.Errorf("clock: advance nil FakeClock")
	}
	if duration < 0 {
		return ErrTimeReversal
	}

	clock.mu.Lock()
	defer clock.mu.Unlock()
	return clock.advanceToLocked(clock.now.Add(duration))
}

// Set moves the clock to timestamp. Moving backward returns ErrTimeReversal.
func (clock *FakeClock) Set(timestamp time.Time) error {
	if clock == nil {
		return fmt.Errorf("clock: set nil FakeClock")
	}

	clock.mu.Lock()
	defer clock.mu.Unlock()
	return clock.advanceToLocked(timestamp.Round(0))
}

// AdvanceNext advances to the next scheduled event. It returns false when no
// active timer or ticker exists.
func (clock *FakeClock) AdvanceNext() (time.Time, bool) {
	if clock == nil {
		return time.Time{}, false
	}

	clock.mu.Lock()
	defer clock.mu.Unlock()

	if len(clock.entries) == 0 {
		return clock.now, false
	}

	timestamp := clock.entries[0].at
	if err := clock.advanceToLocked(timestamp); err != nil {
		return clock.now, false
	}
	return timestamp, true
}

// Pending returns the number of active timers and tickers.
func (clock *FakeClock) Pending() int {
	if clock == nil {
		return 0
	}

	clock.mu.Lock()
	defer clock.mu.Unlock()
	return len(clock.entries)
}

// NextDeadline returns the earliest scheduled event.
func (clock *FakeClock) NextDeadline() (time.Time, bool) {
	if clock == nil {
		return time.Time{}, false
	}

	clock.mu.Lock()
	defer clock.mu.Unlock()
	if len(clock.entries) == 0 {
		return time.Time{}, false
	}
	return clock.entries[0].at, true
}

// BlockUntil waits until at least count active timers or tickers are
// registered. It is useful for synchronizing a test with a goroutine that is
// sleeping on the fake clock.
func (clock *FakeClock) BlockUntil(ctx context.Context, count int) error {
	if ctx == nil {
		return ErrNilContext
	}
	if count <= 0 {
		return nil
	}
	if clock == nil {
		return fmt.Errorf("clock: block on nil FakeClock")
	}

	for {
		clock.mu.Lock()
		if len(clock.entries) >= count {
			clock.mu.Unlock()
			return nil
		}
		changed := clock.changed
		clock.mu.Unlock()

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-changed:
		}
	}
}

func (clock *FakeClock) newEntryLocked(kind scheduleKind) *scheduleEntry {
	clock.nextID++
	return &scheduleEntry{
		id:     clock.nextID,
		kind:   kind,
		ch:     make(chan time.Time, 1),
		index:  -1,
		active: false,
	}
}

func (clock *FakeClock) configureTimerLocked(entry *scheduleEntry, duration time.Duration) {
	clock.unscheduleLocked(entry)
	drain(entry.ch)

	entry.kind = scheduleTimer
	entry.period = 0
	entry.at = clock.now.Add(duration)
	if duration <= 0 {
		entry.active = false
		deliver(entry.ch, clock.now)
		clock.notifyLocked()
		return
	}

	entry.active = true
	heap.Push(&clock.entries, entry)
	clock.notifyLocked()
}

func (clock *FakeClock) configureTickerLocked(entry *scheduleEntry, duration time.Duration) {
	if duration <= 0 {
		panic("clock: non-positive interval for Ticker.Reset")
	}

	clock.unscheduleLocked(entry)
	drain(entry.ch)

	entry.kind = scheduleTicker
	entry.period = duration
	entry.at = clock.now.Add(duration)
	entry.active = true
	heap.Push(&clock.entries, entry)
	clock.notifyLocked()
}

func (clock *FakeClock) stopLocked(entry *scheduleEntry) bool {
	wasActive := entry != nil && entry.active
	clock.unscheduleLocked(entry)
	if entry != nil {
		drain(entry.ch)
	}
	clock.notifyLocked()
	return wasActive
}

func (clock *FakeClock) unscheduleLocked(entry *scheduleEntry) {
	if entry == nil {
		return
	}
	if entry.active && entry.index >= 0 && entry.index < len(clock.entries) {
		heap.Remove(&clock.entries, entry.index)
	}
	entry.active = false
	entry.index = -1
}

func (clock *FakeClock) advanceToLocked(target time.Time) error {
	if target.Before(clock.now) {
		return ErrTimeReversal
	}

	for len(clock.entries) > 0 {
		entry := clock.entries[0]
		if entry.at.After(target) {
			break
		}

		heap.Pop(&clock.entries)
		if !entry.active {
			continue
		}

		clock.now = entry.at
		scheduledAt := entry.at

		switch entry.kind {
		case scheduleTimer:
			entry.active = false
			entry.index = -1
			deliver(entry.ch, scheduledAt)

		case scheduleTicker:
			deliver(entry.ch, scheduledAt)
			remaining := target.Sub(scheduledAt)
			remainder := remaining % entry.period
			delta := entry.period - remainder
			entry.at = target.Add(delta)
			entry.active = true
			heap.Push(&clock.entries, entry)
		}
	}

	clock.now = target
	clock.notifyLocked()
	return nil
}

func (clock *FakeClock) notifyLocked() {
	close(clock.changed)
	clock.changed = make(chan struct{})
}

type scheduleKind uint8

const (
	scheduleTimer scheduleKind = iota + 1
	scheduleTicker
)

type scheduleEntry struct {
	id     uint64
	kind   scheduleKind
	at     time.Time
	period time.Duration
	ch     chan time.Time
	active bool
	index  int
}

type scheduleHeap []*scheduleEntry

func (entries scheduleHeap) Len() int {
	return len(entries)
}

func (entries scheduleHeap) Less(left, right int) bool {
	if entries[left].at.Equal(entries[right].at) {
		return entries[left].id < entries[right].id
	}
	return entries[left].at.Before(entries[right].at)
}

func (entries scheduleHeap) Swap(left, right int) {
	entries[left], entries[right] = entries[right], entries[left]
	entries[left].index = left
	entries[right].index = right
}

func (entries *scheduleHeap) Push(value any) {
	entry := value.(*scheduleEntry)
	entry.index = len(*entries)
	*entries = append(*entries, entry)
}

func (entries *scheduleHeap) Pop() any {
	old := *entries
	last := len(old) - 1
	entry := old[last]
	old[last] = nil
	entry.index = -1
	*entries = old[:last]
	return entry
}

func deliver(channel chan time.Time, timestamp time.Time) {
	select {
	case channel <- timestamp:
	default:
	}
}

func drain(channel chan time.Time) {
	for {
		select {
		case <-channel:
		default:
			return
		}
	}
}

type fakeTimer struct {
	clock *FakeClock
	entry *scheduleEntry
}

var _ Timer = (*fakeTimer)(nil)

func (timer *fakeTimer) C() <-chan time.Time {
	if timer == nil || timer.entry == nil {
		return nil
	}
	return timer.entry.ch
}

func (timer *fakeTimer) Stop() bool {
	if timer == nil || timer.clock == nil || timer.entry == nil {
		return false
	}

	timer.clock.mu.Lock()
	defer timer.clock.mu.Unlock()
	return timer.clock.stopLocked(timer.entry)
}

func (timer *fakeTimer) Reset(duration time.Duration) bool {
	if timer == nil || timer.clock == nil || timer.entry == nil {
		return false
	}

	timer.clock.mu.Lock()
	defer timer.clock.mu.Unlock()
	wasActive := timer.entry.active
	timer.clock.configureTimerLocked(timer.entry, duration)
	return wasActive
}

type fakeTicker struct {
	clock *FakeClock
	entry *scheduleEntry
}

var _ Ticker = (*fakeTicker)(nil)

func (ticker *fakeTicker) C() <-chan time.Time {
	if ticker == nil || ticker.entry == nil {
		return nil
	}
	return ticker.entry.ch
}

func (ticker *fakeTicker) Stop() {
	if ticker == nil || ticker.clock == nil || ticker.entry == nil {
		return
	}

	ticker.clock.mu.Lock()
	defer ticker.clock.mu.Unlock()
	ticker.clock.stopLocked(ticker.entry)
}

func (ticker *fakeTicker) Reset(duration time.Duration) {
	if ticker == nil || ticker.clock == nil || ticker.entry == nil {
		panic("clock: nil fake ticker")
	}
	if duration <= 0 {
		panic("clock: non-positive interval for Ticker.Reset")
	}

	ticker.clock.mu.Lock()
	defer ticker.clock.mu.Unlock()
	ticker.clock.configureTickerLocked(ticker.entry, duration)
}
