// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

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
