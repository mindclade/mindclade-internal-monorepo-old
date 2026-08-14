// Copyright 2026 Mindclade. All rights reserved.
// Confidential, proprietary, and trade-secret information.

package httpxtest

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// Server starts an httptest server and registers cleanup with testing.TB.
func Server(testingTB testing.TB, handler http.Handler) *httptest.Server {
	testingTB.Helper()
	server := httptest.NewServer(handler)
	testingTB.Cleanup(server.Close)
	return server
}
