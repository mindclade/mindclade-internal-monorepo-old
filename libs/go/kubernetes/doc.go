// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

// Package kubernetes provides shared, production-oriented adapters around
// Kubernetes API machinery and controller-runtime.
//
// It owns provider error qualification and small reusable object-management
// primitives. It deliberately does not reimplement controller-runtime queues,
// caches, clients, reconciliation scheduling, or retry behavior.
package kubernetes
