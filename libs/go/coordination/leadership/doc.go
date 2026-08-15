// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

// Package leadership provides lease-backed leader election with fencing,
// bounded renewal, servicekit lifecycle integration, and explicit leadership
// loss semantics. It is suitable for singleton schedulers, reconcilers,
// projectors, dispatch coordinators, and administrative maintenance loops.
package leadership
