// Copyright 2026 Mindclade. All rights reserved.
// Confidential and proprietary.

package observability

import (
	"reflect"
	"strings"
	"unicode"
)

func nilInterface(value any) bool {
	if value == nil {
		return true
	}
	reflected := reflect.ValueOf(value)
	switch reflected.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return reflected.IsNil()
	default:
		return false
	}
}

func validName(value string, maximum int) bool {
	if value == "" || len(value) > maximum || strings.TrimSpace(value) != value {
		return false
	}
	for _, character := range value {
		if unicode.IsControl(character) || unicode.IsSpace(character) {
			return false
		}
	}
	return true
}

func canonicalKey(value string) string {
	normalized := strings.ToLower(strings.TrimSpace(value))
	return strings.NewReplacer("-", "_", ".", "_", " ", "_").Replace(normalized)
}

func sensitiveKey(key string) bool {
	normalized := canonicalKey(key)
	switch normalized {
	case "authorization", "proxy_authorization", "cookie", "set_cookie", "password", "passwd",
		"password_hash", "secret", "client_secret", "token", "access_token", "refresh_token",
		"id_token", "auth_token", "session_token", "api_key", "private_key", "raw_request_body",
		"request_body", "response_body", "model_input", "biological_input":
		return true
	}
	for _, suffix := range []string{"_password", "_passwd", "_secret", "_token", "_api_key", "_private_key"} {
		if strings.HasSuffix(normalized, suffix) {
			return true
		}
	}
	return false
}
