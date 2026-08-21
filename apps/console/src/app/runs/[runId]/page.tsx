// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

import Link from "next/link";
import { RunDetail } from "../../../components/RunDetail";

export default async function Page({ params }: { params: Promise<{ runId: string }> }): Promise<React.ReactNode> {
  const { runId } = await params;
  return <div className="page-stack"><header className="page-heading"><div><Link className="back-link" href="/runs">← Runs</Link><span className="eyebrow">Run detail</span><h1 className="identifier">{runId}</h1><p>Canonical orchestration state and committed outputs.</p></div></header><RunDetail runId={runId} /></div>;
}
