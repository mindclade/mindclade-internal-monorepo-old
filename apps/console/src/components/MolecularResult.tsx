// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

import { MolecularViewer, type Structure } from "@mindclade/libs-ts-molecular-viewer";

export function MolecularResult({ structure }: { structure: Structure }): React.ReactNode {
  return <section className="panel"><div className="panel-heading"><div><h2>Predicted structure</h2><p>{structure.atoms.length.toLocaleString()} atoms · deterministic projection</p></div></div><MolecularViewer structure={structure} /></section>;
}
