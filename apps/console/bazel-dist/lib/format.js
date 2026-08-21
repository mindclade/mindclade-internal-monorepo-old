// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//
const relative = new Intl.RelativeTimeFormat("en", { numeric: "auto" });
export function formatRelativeTime(value, now = Date.now()) {
    const seconds = Math.round((Date.parse(value) - now) / 1_000);
    if (!Number.isFinite(seconds))
        return "Unknown time";
    if (Math.abs(seconds) < 60)
        return relative.format(seconds, "second");
    const minutes = Math.round(seconds / 60);
    if (Math.abs(minutes) < 60)
        return relative.format(minutes, "minute");
    const hours = Math.round(minutes / 60);
    if (Math.abs(hours) < 24)
        return relative.format(hours, "hour");
    return relative.format(Math.round(hours / 24), "day");
}
export function formatBytes(value) {
    if (!Number.isFinite(value) || value < 0)
        return "—";
    const units = ["B", "KiB", "MiB", "GiB", "TiB"];
    const unit = Math.min(Math.floor(Math.log(Math.max(value, 1)) / Math.log(1024)), units.length - 1);
    return `${(value / 1024 ** unit).toLocaleString("en", { maximumFractionDigits: unit === 0 ? 0 : 1 })} ${units[unit]}`;
}
//# sourceMappingURL=format.js.map