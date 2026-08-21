// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

import type { Atom, Structure } from "./StructureLoader.js";

export interface AtomSelection {
  chain?: string;
  residue?: string;
  element?: string;
  residueRange?: readonly [number, number];
}

export function matchesSelection(atom: Atom, selection: AtomSelection): boolean {
  if (selection.chain !== undefined && atom.chain !== selection.chain) return false;
  if (selection.residue !== undefined && atom.residue !== selection.residue) return false;
  if (selection.element !== undefined && atom.element.toUpperCase() !== selection.element.toUpperCase()) return false;
  if (selection.residueRange !== undefined && (atom.residueIndex < selection.residueRange[0] || atom.residueIndex > selection.residueRange[1])) return false;
  return true;
}

export function selectAtoms(structure: Structure, selection: AtomSelection): readonly Atom[] {
  return structure.atoms.filter((atom) => matchesSelection(atom, selection));
}
