// Copyright 2026 Mindclade. All rights reserved.
// Confidential, proprietary, and trade-secret information.

// Package workqueue provides bounded, fenced, durable work leasing for Go
// control-plane processes. It is intended for orchestration commands,
// dispatchers, projectors, ingestion coordination, and administrative jobs;
// it is not a replacement for Kubernetes batch scheduling or model workers.
package workqueue
