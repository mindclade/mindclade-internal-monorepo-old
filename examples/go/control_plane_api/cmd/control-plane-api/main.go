// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package main

import (
	"context"
	"fmt"
	"os"

	controlplaneapi "go.mindclade.dev/examples/go/control_plane_api"
)

func main() {
	address := os.Getenv("MINDCLADE_EXAMPLE_ADDRESS")
	if address == "" {
		address = "127.0.0.1:8080"
	}
	app, err := controlplaneapi.New(controlplaneapi.Config{Address: address})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Printf("control-plane example listening on http://%s\n", app.Address())
	if err := app.Service().RunWithSignals(context.Background()); !controlplaneapi.IsExpectedShutdown(err) {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
