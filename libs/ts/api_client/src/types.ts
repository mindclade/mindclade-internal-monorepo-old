// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

export type ResourceState<T> =
  | { status: "idle" }
  | { status: "loading"; previous?: T }
  | { status: "ready"; data: T; updatedAt: number }
  | { status: "empty"; updatedAt: number }
  | { status: "error"; error: Error; previous?: T };

export type Unsubscribe = () => void;
export type Listener<T> = (value: T) => void;

export interface LoadOptions<T> {
  signal?: AbortSignal;
  isEmpty?: (value: T) => boolean;
}
