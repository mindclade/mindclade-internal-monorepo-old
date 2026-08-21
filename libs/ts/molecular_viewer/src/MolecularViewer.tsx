// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

import type { ReactElement } from "react";
import type { Atom, Structure } from "./StructureLoader.js";
import { matchesSelection, type AtomSelection } from "./Selection.js";

const ELEMENT_COLORS: Readonly<Record<string, string>> = {
  C: "#8793a5", N: "#7bbcff", O: "#ff7e8d", S: "#ffd37a", P: "#f0a6ff", H: "#dfe5ec",
};

export function MolecularViewer({ structure, selection = {}, label, maxVisibleAtoms = 4_000 }: {
  structure: Structure;
  selection?: AtomSelection;
  label?: string;
  maxVisibleAtoms?: number;
}): ReactElement {
  if (!Number.isSafeInteger(maxVisibleAtoms) || maxVisibleAtoms <= 0) throw new RangeError("maxVisibleAtoms must be a positive safe integer");
  const selected = structure.atoms.filter((atom) => matchesSelection(atom, selection));
  const step = Math.max(1, Math.ceil(selected.length / maxVisibleAtoms));
  const atoms = selected.filter((_, index) => index % step === 0);
  const minX = Math.min(...atoms.map((atom) => atom.x), 0); const maxX = Math.max(...atoms.map((atom) => atom.x), 1);
  const minY = Math.min(...atoms.map((atom) => atom.y), 0); const maxY = Math.max(...atoms.map((atom) => atom.y), 1);
  const minZ = Math.min(...atoms.map((atom) => atom.z), 0);
  const position = (atom: Atom): { x: number; y: number } => ({
    x: 24 + ((atom.x - minX) / (maxX - minX || 1)) * 592,
    y: 24 + ((atom.y - minY) / (maxY - minY || 1)) * 312,
  });
  const accessibleLabel = label ?? `${structure.id} molecular structure`;
  return (
    <figure className="mc-molecule" aria-label={accessibleLabel}>
      <svg role="img" aria-label={accessibleLabel} viewBox="0 0 640 360">
        {atoms.sort((left, right) => left.z - right.z).map((atom) => {
          const point = position(atom);
          return <circle key={atom.serial} cx={point.x} cy={point.y} r={2.4 + (atom.z - minZ) / 80} fill={ELEMENT_COLORS[atom.element.toUpperCase()] ?? "#a6ffcb"} opacity=".82"><title>{`${atom.name} · ${atom.residue} ${atom.residueIndex} · chain ${atom.chain}`}</title></circle>;
        })}
      </svg>
      <figcaption className="mc-visually-hidden">{accessibleLabel}; showing {atoms.length} of {selected.length} selected atoms.</figcaption>
    </figure>
  );
}
