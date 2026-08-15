// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

// Package watch provides context-aware consumption of Kubernetes watch
// streams. It owns no relist or resource-version policy; callers decide when
// and how to establish a replacement watch.
package watch
