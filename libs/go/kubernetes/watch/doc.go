// Copyright 2026 Mindclade. All rights reserved.
// Confidential and proprietary.

// Package watch provides context-aware consumption of Kubernetes watch
// streams. It owns no relist or resource-version policy; callers decide when
// and how to establish a replacement watch.
package watch
