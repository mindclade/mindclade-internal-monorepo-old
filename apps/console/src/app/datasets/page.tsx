// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

import { ResourcePage } from "../../components/ResourcePage";
export default function Page(): React.ReactNode { return <ResourcePage kind="datasets" copy={{ eyebrow: "Data plane", title: "Datasets", description: "Immutable dataset releases with lineage, licensing, and quarantine state.", emptyTitle: "No dataset releases", emptyDetail: "Publish a manifest-backed release through the data pipeline." }} />; }
