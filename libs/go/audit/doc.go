// Copyright 2026 Mindclade. All rights reserved.
// Confidential and proprietary.

// Package audit defines immutable, versioned security and control-plane audit
// events for Mindclade services.
//
// It owns the canonical event contract and Recorder interface. It does not own
// persistence, Pub/Sub publication, retention policy, search indexes, or
// service-specific action taxonomies. Storage and transport adapters implement
// Recorder and preserve the event byte-for-byte or semantically losslessly.
package audit
