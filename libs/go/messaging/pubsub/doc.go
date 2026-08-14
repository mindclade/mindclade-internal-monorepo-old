// Copyright 2026 Mindclade. All rights reserved.
// Confidential, proprietary, and trade-secret information.

// Package pubsub adapts an at-least-once publish/subscribe provider to the
// broker-neutral messaging contracts. The provider SDK remains behind narrow
// facades so service composition can pin and qualify the concrete cloud client
// without leaking it into control-plane domain packages.
package pubsub
