// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

import assert from "node:assert/strict";
import test from "node:test";
import { loadPdb, parsePdb, selectAtoms } from "../src/index.js";

const pdb = "ATOM      1  CA  GLY A   1      11.104  13.207   8.000  1.00 20.00           C  ";
test("PDB loader returns selectable atoms", () => {
  const structure = parsePdb(pdb, { id: "1ABC" });
  assert.equal(structure.atoms.length, 1);
  assert.equal(selectAtoms(structure, { chain: "A", element: "C" }).length, 1);
});

test("loadPdb enforces streamed UTF-8 bytes and cancels oversized bodies", async () => {
  let cancelled = false;
  const bytes = new TextEncoder().encode(`${pdb}\n💥`);
  const fetcher: typeof globalThis.fetch = async () => new Response(new ReadableStream<Uint8Array>({
    start(controller) { controller.enqueue(bytes); },
    cancel() { cancelled = true; },
  }));
  await assert.rejects(loadPdb("https://structures.example.test/1abc.pdb", { fetch: fetcher, maxBytes: bytes.byteLength - 1 }), /byte limit/);
  assert.equal(cancelled, true);
});

test("parsePdb validates configured atom bounds", () => {
  assert.throws(() => parsePdb(pdb, { maxAtoms: 0 }), /positive safe integer/);
  assert.throws(() => parsePdb(`${pdb}\n${pdb.replace("     1", "     2")}`, { maxAtoms: 1 }), /atom limit/);
});
