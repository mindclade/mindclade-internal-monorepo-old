// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package middleware

import "net/http"

type SecurityHeadersConfig struct {
	HSTSMaxAgeSeconds     int
	IncludeSubdomains     bool
	ContentSecurityPolicy string
}

// SecurityHeaders applies API-safe response headers. HSTS is disabled unless
// explicitly configured because enabling it on a non-TLS development host can
// create persistent browser failures.
func SecurityHeaders(config SecurityHeadersConfig) Middleware {
	csp := config.ContentSecurityPolicy
	if csp == "" {
		csp = "default-src 'none'; frame-ancestors 'none'; base-uri 'none'"
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			header := writer.Header()
			header.Set("X-Content-Type-Options", "nosniff")
			header.Set("X-Frame-Options", "DENY")
			header.Set("Referrer-Policy", "no-referrer")
			header.Set("Content-Security-Policy", csp)
			if config.HSTSMaxAgeSeconds > 0 {
				value := "max-age=" + itoa(config.HSTSMaxAgeSeconds)
				if config.IncludeSubdomains {
					value += "; includeSubDomains"
				}
				header.Set("Strict-Transport-Security", value)
			}
			next.ServeHTTP(writer, request)
		})
	}
}

func itoa(value int) string {
	if value == 0 {
		return "0"
	}
	negative := value < 0
	if negative {
		value = -value
	}
	buffer := [32]byte{}
	index := len(buffer)
	for value > 0 {
		index--
		buffer[index] = byte('0' + value%10)
		value /= 10
	}
	if negative {
		index--
		buffer[index] = '-'
	}
	return string(buffer[index:])
}
