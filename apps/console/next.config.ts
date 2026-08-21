// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

import type { NextConfig } from "next";

const nextConfig: NextConfig = {
  poweredByHeader: false,
  reactStrictMode: true,
  transpilePackages: [
    "@mindclade/libs-ts-api-client",
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
        headers: [
          { key: "Referrer-Policy", value: "strict-origin-when-cross-origin" },
          { key: "X-Content-Type-Options", value: "nosniff" },
          { key: "X-Frame-Options", value: "DENY" },
          { key: "Permissions-Policy", value: "camera=(), microphone=(), geolocation=()" },
        ],
      },
    ];
  },
};

export default nextConfig;
