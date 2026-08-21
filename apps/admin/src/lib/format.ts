// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

export function formatAuditTime(value: string): string {
  const date = new Date(value);
  return Number.isNaN(date.valueOf()) ? "Unknown" : new Intl.DateTimeFormat("en", { dateStyle: "medium", timeStyle: "long" }).format(date);
}

export function shortIdentity(value: string): string {
  return value.length <= 30 ? value : `${value.slice(0, 14)}…${value.slice(-10)}`;
}
