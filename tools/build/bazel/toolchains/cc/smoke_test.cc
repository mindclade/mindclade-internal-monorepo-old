// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

#include <array>
#include <filesystem>
#include <numeric>
#include <string>

#if defined(__APPLE__)
#include <TargetConditionals.h>
#elif defined(__linux__)
#include <unistd.h>
#else
#error "The Mindclade C/C++ toolchain supports Darwin and Linux only"
#endif

int main() {
  const std::array<int, 4> values = {1, 2, 3, 4};
  const auto total = std::accumulate(values.begin(), values.end(), 0);
  const std::filesystem::path evidence = "mindclade";
  return total == 10 && evidence.string() == std::string("mindclade") ? 0 : 1;
}
