// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

import { Metric, StatusBadge } from "@mindclade/libs-ts-design-system";
import type { Evaluation } from "@mindclade/sdk-typescript";

export function EvaluationSummary({ evaluation }: { evaluation: Evaluation }): React.ReactNode {
  return <section className="panel"><div className="panel-heading"><div><span className="eyebrow">Evaluation suite</span><h2>{evaluation.suite}</h2></div><StatusBadge tone={evaluation.status === "PASSED" ? "success" : evaluation.status === "FAILED" ? "danger" : "warning"}>{evaluation.status}</StatusBadge></div><div className="metric-grid">{evaluation.metrics.map((metric) => <Metric key={metric.name} label={metric.name} value={metric.value.toLocaleString()} detail={`${metric.unit}${metric.threshold === undefined ? "" : ` · threshold ${metric.threshold}`}`} trend={metric.passed ? "up" : "down"} />)}</div><p className="evidence">Evidence <code>{evaluation.evidenceDigest}</code>{evaluation.holdoutProtected ? " · holdout protected" : ""}</p></section>;
}
