// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

export interface Atom {
  serial: number;
  name: string;
  element: string;
  residue: string;
  residueIndex: number;
  chain: string;
  x: number;
  y: number;
  z: number;
}

export interface Structure { id: string; atoms: readonly Atom[] }

export function parsePdb(source: string, options: { id?: string; maxAtoms?: number } = {}): Structure {
  const maxAtoms = options.maxAtoms ?? 100_000;
  const atoms: Atom[] = [];
  for (const line of source.split(/\r?\n/)) {
    const record = line.slice(0, 6).trim();
    if (record !== "ATOM" && record !== "HETATM") continue;
    if (atoms.length >= maxAtoms) throw new RangeError(`Structure exceeds the ${maxAtoms} atom limit`);
    const atom: Atom = {
      serial: Number.parseInt(line.slice(6, 11).trim(), 10),
      name: line.slice(12, 16).trim(),
      residue: line.slice(17, 20).trim(),
      chain: line.slice(21, 22).trim() || "_",
      residueIndex: Number.parseInt(line.slice(22, 26).trim(), 10),
      x: Number.parseFloat(line.slice(30, 38)),
      y: Number.parseFloat(line.slice(38, 46)),
      z: Number.parseFloat(line.slice(46, 54)),
      element: line.slice(76, 78).trim() || line.slice(12, 14).trim().replace(/\d/g, ""),
    };
    if ([atom.serial, atom.residueIndex, atom.x, atom.y, atom.z].every(Number.isFinite)) atoms.push(atom);
  }
  return { id: options.id ?? "structure", atoms };
}

export async function loadPdb(url: URL | string, options: { signal?: AbortSignal; maxBytes?: number } = {}): Promise<Structure> {
  const response = await fetch(url, { ...(options.signal === undefined ? {} : { signal: options.signal }) });
  if (!response.ok) throw new Error(`Structure request failed with status ${response.status}`);
  const length = Number(response.headers.get("content-length") ?? 0);
  const maxBytes = options.maxBytes ?? 20_000_000;
  if (length > maxBytes) throw new RangeError(`Structure exceeds the ${maxBytes} byte limit`);
  const source = await response.text();
  if (source.length > maxBytes) throw new RangeError(`Structure exceeds the ${maxBytes} byte limit`);
  return parsePdb(source, { id: new URL(String(url), "http://localhost").pathname.split("/").at(-1) ?? "structure" });
}
