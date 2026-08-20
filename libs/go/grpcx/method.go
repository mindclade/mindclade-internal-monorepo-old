// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package grpcx

import (
	"strings"

	"go.mindclade.dev/libs/go/faults"
	"go.mindclade.dev/libs/go/requestmeta"
)

const maximumMethodLength = 1024

type Method struct {
	Full    string
	Service string
	Name    string
}

func ParseMethod(value string) (Method, error) {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > maximumMethodLength || !strings.HasPrefix(value, "/") || strings.Count(value, "/") != 2 {
		return Method{}, invalidMethod()
	}
	parts := strings.Split(strings.TrimPrefix(value, "/"), "/")
	if len(parts) != 2 || !validServiceName(parts[0]) || !validIdentifier(parts[1]) {
		return Method{}, invalidMethod()
	}
	return Method{Full: value, Service: parts[0], Name: parts[1]}, nil
}

func (method Method) Operation() (requestmeta.Operation, error) {
	parsed, err := ParseMethod(method.Full)
	if err != nil {
		return requestmeta.Operation{}, err
	}
	return requestmeta.ParseOperation("rpc." + parsed.Service + "." + parsed.Name)
}

func validServiceName(value string) bool {
	if value == "" || len(value) > 768 {
		return false
	}
	segments := strings.Split(value, ".")
	for _, segment := range segments {
		if !validIdentifier(segment) {
			return false
		}
	}
	return true
}

func validIdentifier(value string) bool {
	if value == "" || len(value) > 512 {
		return false
	}
	for index := 0; index < len(value); index++ {
		character := value[index]
		if index == 0 {
			if !asciiLetter(character) && character != '_' {
				return false
			}
			continue
		}
		if !asciiLetter(character) && (character < '0' || character > '9') && character != '_' {
			return false
		}
	}
	return true
}

func asciiLetter(value byte) bool {
	return value >= 'A' && value <= 'Z' || value >= 'a' && value <= 'z'
}

func invalidMethod() error {
	return faults.Wrap(
		ErrInvalidMethod,
		faults.CodeInvalidArgument,
		"invalid gRPC method",
		faults.WithReason("invalid_grpc_method"),
		faults.WithOperation("grpcx.ParseMethod"),
	)
}
