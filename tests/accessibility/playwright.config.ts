// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

import { defineConfig, devices } from "@playwright/test";

const localChromium = process.platform === "darwin" ? { channel: "chrome" as const } : {};

export default defineConfig({
  testDir: ".",
  testMatch: "apps.spec.ts",
  fullyParallel: true,
  forbidOnly: Boolean(process.env.CI),
  retries: process.env.CI ? 1 : 0,
  workers: process.env.CI ? 2 : undefined,
  reporter: [["line"]],
  expect: { timeout: 10_000 },
  use: {
    trace: process.env.CI ? "on-first-retry" : "off",
    screenshot: "only-on-failure",
    video: "off",
  },
  webServer: [
    {
      command: "pnpm --dir ../../apps/console start --hostname 127.0.0.1 --port 4411",
      url: "http://127.0.0.1:4411",
      reuseExistingServer: !process.env.CI,
      timeout: 60_000,
    },
    {
      command: "pnpm --dir ../../apps/admin start --hostname 127.0.0.1 --port 4412",
      url: "http://127.0.0.1:4412",
      reuseExistingServer: !process.env.CI,
      timeout: 60_000,
    },
  ],
  projects: [
    { name: "chromium", use: { ...devices["Desktop Chrome"], ...localChromium } },
    { name: "firefox", use: { ...devices["Desktop Firefox"] } },
    { name: "webkit", use: { ...devices["Desktop Safari"] } },
    { name: "chromium-reflow", use: { browserName: "chromium", viewport: { width: 600, height: 800 }, ...localChromium } },
    { name: "mobile-chromium", use: { ...devices["Pixel 7"], ...localChromium } },
  ],
});
