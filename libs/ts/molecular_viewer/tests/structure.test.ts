// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

import assert from "node:assert/strict";
import test from "node:test";
import { parsePdb, selectAtoms } from "../src/index.js";

const pdb = "ATOM      1  CA  GLY A   1      11.104  13.207   8.000  1.00 20.00           C  ";
test("PDB loader returns selectable atoms", () => {
  const structure = parsePdb(pdb, { id: "1ABC" });
  assert.equal(structure.atoms.length, 1);
  assert.equal(selectAtoms(structure, { chain: "A", element: "C" }).length, 1);
});
