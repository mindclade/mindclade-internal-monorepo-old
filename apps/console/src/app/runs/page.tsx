// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

import { ResourcePage } from "../../components/ResourcePage";
export default function Page(): React.ReactNode { return <ResourcePage kind="runs" copy={{ eyebrow: "Orchestration", title: "Runs", description: "Durable work from queue admission through verified outputs.", emptyTitle: "No runs in this workspace", emptyDetail: "Start a bounded run when its configuration artifact is ready.", action: "New run" }} />; }
