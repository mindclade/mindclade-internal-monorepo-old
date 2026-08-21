// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

import { ResourcePage } from "../../components/ResourcePage";
export default function Page(): React.ReactNode { return <ResourcePage kind="evaluations" copy={{ eyebrow: "Independent assurance", title: "Evaluations", description: "Capability and safety evidence kept independent from model production.", emptyTitle: "No evaluations recorded", emptyDetail: "Run a governed suite against a candidate model.", action: "New evaluation" }} />; }
