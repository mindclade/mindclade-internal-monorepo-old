// Copyright 2026 Mindclade. All rights reserved.
// Confidential, proprietary, and trade-secret information.

package connecttest

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func Server(testingTB testing.TB, handler http.Handler) *httptest.Server {
	testingTB.Helper()
	server := httptest.NewUnstartedServer(handler)
	server.EnableHTTP2 = true
	server.StartTLS()
	testingTB.Cleanup(server.Close)
	return server
}
