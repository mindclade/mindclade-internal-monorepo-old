// Copyright 2026 Mindclade. All rights reserved.
// Confidential, proprietary, and trade-secret information.

package connectx

import (
	"mindclade.internal/libs/go/httpx"
	"net/http"
)

// HeaderCarrier uses the same canonical request-lineage header mapping as
// ordinary HTTP endpoints.
type HeaderCarrier = httpx.HeaderCarrier

func NewHeaderCarrier(header http.Header) HeaderCarrier { return HeaderCarrier{Header: header} }
