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
  if (!Number.isSafeInteger(maxAtoms) || maxAtoms <= 0) throw new RangeError("maxAtoms must be a positive safe integer");
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

export async function loadPdb(url: URL | string, options: {
  signal?: AbortSignal;
  maxBytes?: number;
  maxAtoms?: number;
  fetch?: typeof globalThis.fetch;
} = {}): Promise<Structure> {
  const maxBytes = options.maxBytes ?? 20_000_000;
  if (!Number.isSafeInteger(maxBytes) || maxBytes <= 0) throw new RangeError("maxBytes must be a positive safe integer");
  const response = await (options.fetch ?? globalThis.fetch)(url, { ...(options.signal === undefined ? {} : { signal: options.signal }) });
  if (!response.ok) throw new Error(`Structure request failed with status ${response.status}`);
  const length = Number(response.headers.get("content-length") ?? 0);
  if (Number.isFinite(length) && length > maxBytes) {
    await response.body?.cancel().catch(() => undefined);
    throw new RangeError(`Structure exceeds the ${maxBytes} byte limit`);
  }
  const source = await readBoundedText(response, maxBytes);
  return parsePdb(source, {
    id: new URL(String(url), "http://localhost").pathname.split("/").at(-1) ?? "structure",
    ...(options.maxAtoms === undefined ? {} : { maxAtoms: options.maxAtoms }),
  });
}

async function readBoundedText(response: Response, maxBytes: number): Promise<string> {
  if (response.body === null) return "";
  const reader = response.body.getReader();
  const decoder = new TextDecoder();
  let bytes = 0;
  let text = "";
  let completed = false;
  try {
    for (;;) {
      const { value, done } = await reader.read();
      if (done) {
        text += decoder.decode();
        completed = true;
        return text;
      }
      bytes += value.byteLength;
      if (bytes > maxBytes) throw new RangeError(`Structure exceeds the ${maxBytes} byte limit`);
      text += decoder.decode(value, { stream: true });
    }
  } finally {
    if (!completed) await reader.cancel().catch(() => undefined);
    reader.releaseLock();
  }
}
