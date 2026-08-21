// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

export interface ConsoleIdentity {
  displayName: string;
  organization: string;
  environment: string;
}

export const anonymousIdentity: ConsoleIdentity = {
  displayName: "Workspace user",
  organization: "Mindclade",
  environment: process.env.NEXT_PUBLIC_ENVIRONMENT ?? "local",
};
