// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

import assert from "node:assert/strict";
import test from "node:test";
import { ResourceStore, boundedPageSize } from "../src/index.js";

test("ResourceStore publishes ready and empty state", async () => {
  const store = new ResourceStore<readonly string[]>();
  let changes = 0;
  const unsubscribe = store.subscribe(() => { changes += 1; });
  await store.load(async () => [], { isEmpty: (items) => items.length === 0 });
  assert.equal(store.getSnapshot().status, "empty");
  assert.equal(changes, 2);
  unsubscribe();
});

test("boundedPageSize enforces safe limits", () => {
  assert.equal(boundedPageSize(0), 1);
  assert.equal(boundedPageSize(1_000, 80), 80);
});
