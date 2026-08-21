// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

import AxeBuilder from "@axe-core/playwright";
import { expect, test } from "@playwright/test";
import { browserSecurityHeaders } from "@mindclade/libs-ts-browser-security";
import { loopbackDocumentHeaders } from "./loopback-security.js";

const surfaces = [
  { name: "Command", origin: "http://127.0.0.1:4411", routes: ["/", "/runs", "/evaluations", "/safety"] },
  { name: "Governance", origin: "http://127.0.0.1:4412", routes: ["/", "/releases", "/break-glass", "/audit"] },
] as const;

function violationSummary(violations: Awaited<ReturnType<AxeBuilder["analyze"]>>["violations"]): string {
  return violations.flatMap((violation) => violation.nodes.map((node) =>
    `${violation.id} (${violation.impact ?? "unknown"}) ${node.target.join(" ")}: ${node.failureSummary ?? violation.help}`,
  )).join("\n");
}

test.beforeEach(async ({ page }) => {
  await page.route(/^http:\/\/127\.0\.0\.1:441[12](?:\/|$)/, async (route) => {
    if (route.request().resourceType() !== "document") {
      await route.continue();
      return;
    }
    const response = await route.fetch();
    await route.fulfill({
      response,
      headers: loopbackDocumentHeaders(route.request().url(), response.headers()),
    });
  });
});

test("accessibility transport override is loopback-only and preserves production defaults", async ({}, testInfo) => {
  test.skip(testInfo.project.name !== "chromium", "single contract assertion");
  const production = Object.fromEntries(browserSecurityHeaders().map(({ key, value }) => [key, value]));
  expect(production["Content-Security-Policy"]).toContain("upgrade-insecure-requests");
  expect(production["Strict-Transport-Security"]).toContain("includeSubDomains");

  const loopback = loopbackDocumentHeaders("http://127.0.0.1:4411/", production);
  expect(loopback["Content-Security-Policy"]).not.toContain("upgrade-insecure-requests");
  expect(loopback["Strict-Transport-Security"]).toBeUndefined();
  expect(production["Content-Security-Policy"]).toContain("upgrade-insecure-requests");
  expect(() => loopbackDocumentHeaders("https://console.mindclade.example/", production)).toThrow(/non-test origin/);
});

for (const surface of surfaces) {
  for (const route of surface.routes) {
    test(`${surface.name} ${route} has no automated WCAG 2.1 AA violations`, async ({ page }) => {
      await page.goto(`${surface.origin}${route}`);
      await expect(page.locator("main")).toBeVisible();
      await expect(page.locator("h1")).toHaveCount(1);
      await expect(page.locator('[data-session]:not([data-session="loading"])').first()).toBeAttached();
      const results = await new AxeBuilder({ page }).withTags([
        "wcag2a",
        "wcag2aa",
        "wcag21a",
        "wcag21aa",
      ]).analyze();
      expect(results.violations.length, violationSummary(results.violations)).toBe(0);
    });
  }

  test(`${surface.name} skip navigation is first and moves focus`, async ({ page }) => {
    await page.goto(surface.origin);
    await page.keyboard.press("Tab");
    const skipLink = page.getByRole("link", { name: "Skip to content" });
    await expect(skipLink).toBeFocused();
    await page.keyboard.press("Enter");
    await expect(page.locator("main")).toBeFocused();
  });

  test(`${surface.name} mobile navigation has a touch target and Escape return`, async ({ page }, testInfo) => {
    test.skip(testInfo.project.name !== "mobile-chromium", "mobile interaction coverage");
    await page.goto(surface.origin);
    const toggle = page.locator(".mobile-nav-toggle");
    await expect(toggle).toHaveAccessibleName("Menu");
    const bounds = await toggle.boundingBox();
    expect(bounds?.width ?? 0).toBeGreaterThanOrEqual(44);
    expect(bounds?.height ?? 0).toBeGreaterThanOrEqual(44);
    await toggle.click();
    await expect(toggle).toHaveAttribute("aria-expanded", "true");
    await page.keyboard.press("Escape");
    await expect(toggle).toHaveAttribute("aria-expanded", "false");
    await expect(toggle).toBeFocused();
  });

  test(`${surface.name} reflows without horizontal page overflow`, async ({ page }, testInfo) => {
    test.skip(testInfo.project.name !== "chromium-reflow", "200 percent equivalent reflow coverage");
    await page.goto(surface.origin);
    const hasOverflow = await page.evaluate(() =>
      document.documentElement.scrollWidth > document.documentElement.clientWidth,
    );
    expect(hasOverflow).toBe(false);
  });
}
