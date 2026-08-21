// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

import Link from "next/link";
import { EvaluationDetail } from "../../../components/EvaluationDetail";

export default async function Page({ params }: { params: Promise<{ evaluationId: string }> }): Promise<React.ReactNode> {
  const { evaluationId } = await params;
  return <div className="page-stack"><header className="page-heading"><div><Link className="back-link" href="/evaluations">← Evaluations</Link><span className="eyebrow">Evaluation evidence</span><h1 className="identifier">{evaluationId}</h1><p>Independent gate results and immutable evidence identity.</p></div></header><EvaluationDetail evaluationId={evaluationId} /></div>;
}
