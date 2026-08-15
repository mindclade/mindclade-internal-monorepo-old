// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

// Package pubsub adapts an at-least-once publish/subscribe provider to the
// broker-neutral messaging contracts. The provider SDK remains behind narrow
// facades so service composition can pin and qualify the concrete cloud client
// without leaking it into control-plane domain packages.
package pubsub
