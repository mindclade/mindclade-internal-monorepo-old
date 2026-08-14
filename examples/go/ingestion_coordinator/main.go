// Copyright 2026 Mindclade. All rights reserved.
// Confidential, proprietary, and trade-secret information.

package main

import (
	"context"
	"fmt"
	"os"
	"time"
)

func main() {
	application, err := BuildApplication(context.Background())
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := application.Start(context.Background()); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	select {
	case message := <-application.Received:
		fmt.Printf("published %s for work item %s\n", message.Topic(), application.ItemID.String())
		application.Shutdown()
	case <-time.After(10 * time.Second):
		application.Shutdown()
		fmt.Fprintln(os.Stderr, "timed out waiting for the local ingestion event")
		os.Exit(1)
	}
	waitCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := application.Wait(waitCtx); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
