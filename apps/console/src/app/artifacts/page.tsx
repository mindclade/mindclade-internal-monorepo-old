// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

import { ResourcePage } from "../../components/ResourcePage";
export default function Page(): React.ReactNode { return <ResourcePage kind="artifacts" copy={{ eyebrow: "Artifact plane", title: "Artifacts", description: "Content-addressed evidence, outputs, checkpoints, and manifests.", emptyTitle: "No artifacts visible", emptyDetail: "Verified outputs appear when a producing run commits its manifest." }} />; }
