// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package connectx

import (
	"mindclade.internal/libs/go/httpx"
	"net/http"
)

// HeaderCarrier uses the same canonical request-lineage header mapping as
// ordinary HTTP endpoints.
type HeaderCarrier = httpx.HeaderCarrier

func NewHeaderCarrier(header http.Header) HeaderCarrier { return HeaderCarrier{Header: header} }
