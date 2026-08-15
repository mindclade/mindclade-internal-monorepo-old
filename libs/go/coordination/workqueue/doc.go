// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

// Package workqueue provides bounded, fenced, durable work leasing for Go
// control-plane processes. It is intended for orchestration commands,
// dispatchers, projectors, ingestion coordination, and administrative jobs;
// it is not a replacement for Kubernetes batch scheduling or model workers.
package workqueue
