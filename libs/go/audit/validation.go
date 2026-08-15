// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package audit

import (
	"reflect"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	MaximumActionLength     = 160
	MaximumReasonLength     = 128
	MaximumTargetTypeLength = 96
	MaximumTargetNameLength = 256
)

func validCanonicalName(value string, maximum int, requireSeparator bool) bool {
	if value == "" || len(value) > maximum {
		return false
	}
	hasSeparator := false
	previousSeparator := false
	for index, character := range value {
		isLetter := character >= 'a' && character <= 'z'
		isDigit := character >= '0' && character <= '9'
		isSeparator := strings.ContainsRune("._:-/", character)
		if !isLetter && !isDigit && !isSeparator {
			return false
		}
		if index == 0 && !isLetter {
			return false
		}
		if index == len(value)-1 && !isLetter && !isDigit {
			return false
		}
		if isSeparator && previousSeparator {
			return false
		}
		hasSeparator = hasSeparator || isSeparator
		previousSeparator = isSeparator
	}
	return !requireSeparator || hasSeparator
}

func validDisplayText(value string, maximum int) bool {
	if len(value) > maximum || !utf8.ValidString(value) {
		return false
	}
	for _, character := range value {
		if unicode.IsControl(character) {
			return false
		}
	}
	return true
}

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
