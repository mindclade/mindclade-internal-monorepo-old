// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

import { browserSecurityHeaders } from "@mindclade/libs-ts-browser-security";
import type { NextConfig } from "next";

const development = process.env.NODE_ENV !== "production";

const nextConfig: NextConfig = {
  poweredByHeader: false,
  reactStrictMode: true,
  output: "standalone",
  transpilePackages: [
    "@mindclade/libs-ts-api-client",
    "@mindclade/libs-ts-auth",
    "@mindclade/libs-ts-browser-security",
    "@mindclade/libs-ts-charts",
    "@mindclade/libs-ts-design-system",
    "@mindclade/libs-ts-molecular-viewer",
    "@mindclade/libs-ts-telemetry",
    "@mindclade/sdk-typescript",
  ],
  experimental: {
    optimizePackageImports: [
      "@mindclade/libs-ts-charts",
      "@mindclade/libs-ts-design-system",
      "@mindclade/sdk-typescript",
    ],
  },
  async headers() {
    return [
      {
        source: "/:path*",
        headers: [...browserSecurityHeaders({
          development,
          connectEndpoints: [process.env.NEXT_PUBLIC_API_BASE_URL, process.env.NEXT_PUBLIC_TELEMETRY_ENDPOINT],
        })],
      },
    ];
  },
};

export default nextConfig;
