// Copyright 2026 Mindclade. All rights reserved.
// Confidential and proprietary.

package identifiers

import (
	cryptorand "crypto/rand"
	"fmt"
	"io"
	"sync"
	"time"
)

const maximumUUIDv7Milliseconds = uint64(1<<48 - 1)

// Generator creates UUIDv4, monotonically ordered UUIDv7, and resource IDs.
// It is safe for concurrent use.
//
// Within one Generator, UUIDv7 values are strictly increasing even when
// multiple values are generated in the same millisecond or the supplied wall
// clock moves backward. Different processes remain probabilistically unique
// through cryptographic randomness; no cross-process ordering is promised.
type Generator struct {
	mu          sync.Mutex
	now         func() time.Time
	entropy     io.Reader
	initialized bool
	last        UUID
	lastMillis  uint64
}

// GeneratorOption configures a Generator.
type GeneratorOption func(*Generator) error

// WithTimeSource replaces time.Now. The source is called while the Generator
// mutex is held and must return promptly.
func WithTimeSource(source func() time.Time) GeneratorOption {
	return func(generator *Generator) error {
		if source == nil {
			return invalidValue("time source", "", "must not be nil", ErrTimeRange)
		}
		generator.now = source
		return nil
	}
}

// WithEntropySource replaces crypto/rand.Reader. The Generator serializes
// reads, so a custom source need not be concurrency-safe.
func WithEntropySource(source io.Reader) GeneratorOption {
	return func(generator *Generator) error {
		if source == nil {
			return invalidValue("entropy source", "", "must not be nil", ErrEntropy)
		}
		generator.entropy = source
		return nil
	}
}

// NewGenerator constructs a generator using time.Now and crypto/rand.Reader.
func NewGenerator(options ...GeneratorOption) (*Generator, error) {
	generator := &Generator{
		now:     time.Now,
		entropy: cryptorand.Reader,
	}
	for _, option := range options {
		if option == nil {
			continue
		}
		if err := option(generator); err != nil {
			return nil, err
		}
	}
	return generator, nil
}

var defaultGenerator = func() *Generator {
	generator, err := NewGenerator()
	if err != nil {
		panic(err)
	}
	return generator
}()

// NewUUIDv4 returns an RFC variant, randomly generated version 4 UUID.
func NewUUIDv4() (UUID, error) {
	return defaultGenerator.UUIDv4()
}

// NewUUIDv7 returns a time-ordered version 7 UUID.
func NewUUIDv7() (UUID, error) {
	return defaultGenerator.UUIDv7()
}

// NewUUIDv7At returns a version 7 UUID using timestamp. The package-level
// generator still enforces monotonic ordering relative to its prior output.
func NewUUIDv7At(timestamp time.Time) (UUID, error) {
	return defaultGenerator.UUIDv7At(timestamp)
}

// UUIDv4 returns an RFC variant, randomly generated version 4 UUID.
func (generator *Generator) UUIDv4() (UUID, error) {
	if generator == nil {
		return UUID{}, invalidValue("generator", "", "must not be nil", ErrEntropy)
	}

	generator.mu.Lock()
	defer generator.mu.Unlock()

	var uuid UUID
	if _, err := io.ReadFull(generator.entropy, uuid[:]); err != nil {
		return UUID{}, fmt.Errorf("%w: uuidv4: %v", ErrEntropy, err)
	}
	uuid[6] = uuid[6]&0x0F | 0x40
	uuid[8] = uuid[8]&0x3F | 0x80
	return uuid, nil
}

// UUIDv7 returns a version 7 UUID using the configured time source.
func (generator *Generator) UUIDv7() (UUID, error) {
	if generator == nil {
		return UUID{}, invalidValue("generator", "", "must not be nil", ErrEntropy)
	}

	generator.mu.Lock()
	defer generator.mu.Unlock()

	milliseconds, err := uuidv7Milliseconds(generator.now())
	if err != nil {
		return UUID{}, err
	}
	return generator.uuidv7Locked(milliseconds)
}

// UUIDv7At returns a monotonically increasing version 7 UUID using timestamp.
func (generator *Generator) UUIDv7At(timestamp time.Time) (UUID, error) {
	if generator == nil {
		return UUID{}, invalidValue("generator", "", "must not be nil", ErrEntropy)
	}

	milliseconds, err := uuidv7Milliseconds(timestamp)
	if err != nil {
		return UUID{}, err
	}

	generator.mu.Lock()
	defer generator.mu.Unlock()
	return generator.uuidv7Locked(milliseconds)
}

func (generator *Generator) uuidv7Locked(milliseconds uint64) (UUID, error) {
	if !generator.initialized || milliseconds > generator.lastMillis {
		uuid, err := generator.randomUUIDv7Locked(milliseconds)
		if err != nil {
			return UUID{}, err
		}
		generator.last = uuid
		generator.lastMillis = milliseconds
		generator.initialized = true
		return uuid, nil
	}

	// Preserve monotonicity across same-millisecond generation and wall-clock
	// regression by incrementing the prior 74-bit random field.
	uuid := generator.last
	if !incrementUUIDv7Random(&uuid) {
		if generator.lastMillis == maximumUUIDv7Milliseconds {
			return UUID{}, fmt.Errorf("%w: monotonic uuidv7 state exhausted", ErrTimeRange)
		}
		milliseconds = generator.lastMillis + 1
		var err error
		uuid, err = generator.randomUUIDv7Locked(milliseconds)
		if err != nil {
			return UUID{}, err
		}
	} else {
		milliseconds = generator.lastMillis
	}

	generator.last = uuid
	generator.lastMillis = milliseconds
	generator.initialized = true
	return uuid, nil
}

// ID creates a resource ID of kind using the configured time source.
func (generator *Generator) ID(kind Kind) (ID, error) {
	if err := kind.Validate(); err != nil {
		return ID{}, err
	}
	uuid, err := generator.UUIDv7()
	if err != nil {
		return ID{}, err
	}
	return ID{kind: kind, uuid: uuid}, nil
}

// IDAt creates a resource ID of kind using timestamp.
func (generator *Generator) IDAt(kind Kind, timestamp time.Time) (ID, error) {
	if err := kind.Validate(); err != nil {
		return ID{}, err
	}
	uuid, err := generator.UUIDv7At(timestamp)
	if err != nil {
		return ID{}, err
	}
	return ID{kind: kind, uuid: uuid}, nil
}

func (generator *Generator) randomUUIDv7Locked(milliseconds uint64) (UUID, error) {
	var uuid UUID
	uuid[0] = byte(milliseconds >> 40)
	uuid[1] = byte(milliseconds >> 32)
	uuid[2] = byte(milliseconds >> 24)
	uuid[3] = byte(milliseconds >> 16)
	uuid[4] = byte(milliseconds >> 8)
	uuid[5] = byte(milliseconds)

	if _, err := io.ReadFull(generator.entropy, uuid[6:]); err != nil {
		return UUID{}, fmt.Errorf("%w: uuidv7: %v", ErrEntropy, err)
	}
	uuid[6] = uuid[6]&0x0F | 0x70
	uuid[8] = uuid[8]&0x3F | 0x80
	return uuid, nil
}

func uuidv7Milliseconds(timestamp time.Time) (uint64, error) {
	milliseconds := timestamp.UnixMilli()
	if milliseconds < 0 || uint64(milliseconds) > maximumUUIDv7Milliseconds {
		return 0, invalidValue(
			"uuidv7 timestamp",
			timestamp.UTC().Format(time.RFC3339Nano),
			"must fit the unsigned 48-bit Unix millisecond field",
			ErrTimeRange,
		)
	}
	return uint64(milliseconds), nil
}

func incrementUUIDv7Random(uuid *UUID) bool {
	for index := 15; index >= 9; index-- {
		uuid[index]++
		if uuid[index] != 0 {
			return true
		}
	}

	lowSix := uuid[8] & 0x3F
	if lowSix < 0x3F {
		uuid[8] = 0x80 | lowSix + 1
		return true
	}
	uuid[8] = 0x80

	if uuid[7] < 0xFF {
		uuid[7]++
		return true
	}
	uuid[7] = 0

	lowFour := uuid[6] & 0x0F
	if lowFour < 0x0F {
		uuid[6] = 0x70 | lowFour + 1
		return true
	}
	uuid[6] = 0x70
	return false
}
