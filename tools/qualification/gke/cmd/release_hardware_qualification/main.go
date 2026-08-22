// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"go.mindclade.dev/tools/qualification/gke/probe"
)

func main() {
	profile := flag.String("profile", "", "exact qualification profile: cpu, h100, or b200")
	scratch := flag.String("scratch", "", "absolute bounded scratch directory")
	contract := flag.String("contract", "", "qualification contract version")
	runID := flag.String("run-id", "", "bounded unique qualification run identifier")
	sourceRevision := flag.String("source-revision", "", "exact 40-hex source revision")
	image := flag.String("image", "", "running image pinned by nonzero sha256 digest")
	requireComplete := flag.Bool("require-complete", false, "reject any partial qualification path")
	timeout := flag.Duration("timeout", 50*time.Minute, "overall local qualification timeout")
	flag.Parse()
	if flag.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "foundation GKE qualification failed: positional arguments are forbidden")
		os.Exit(2)
	}
	if *timeout < time.Minute || *timeout > time.Hour {
		fmt.Fprintln(os.Stderr, "foundation GKE qualification failed: timeout must be between 1m and 1h")
		os.Exit(2)
	}
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	result, err := probe.Execute(ctx, probe.Config{
		Profile:         *profile,
		Scratch:         *scratch,
		Contract:        *contract,
		RunID:           *runID,
		SourceRevision:  *sourceRevision,
		Image:           *image,
		RequireComplete: *requireComplete,
	}, probe.Dependencies{})
	if err != nil {
		fmt.Fprintf(os.Stderr, "foundation GKE qualification failed: %v\n", err)
		os.Exit(1)
	}
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(result); err != nil {
		fmt.Fprintf(os.Stderr, "foundation GKE qualification failed: encode result: %v\n", err)
		os.Exit(1)
	}
}
