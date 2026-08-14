// Copyright 2026 Mindclade. All rights reserved.
// Confidential and proprietary.

package auth

import (
	"errors"
	"sort"
	"strings"

	"mindclade.internal/libs/go/faults"
)

const (
	MaximumPermissionLength = 128
	MaximumPermissions      = 512
)

// Permission is a canonical lower-case dotted capability, for example
// "runs.read", "models.release.promote", or the grant "runs.*".
type Permission string

func ParsePermission(value string) (Permission, error) {
	normalized := strings.TrimSpace(value)
	permission := Permission(normalized)
	if !permission.Valid() {
		return "", newFault(
			errors.Join(ErrInvalidPermission, errors.New("invalid grammar")),
			faults.CodeInvalidArgument,
			"invalid permission",
			"invalid_permission",
			"auth.ParsePermission",
			faults.Fields{"permission": normalized},
		)
	}
	return permission, nil
}

func MustParsePermission(value string) Permission {
	permission, err := ParsePermission(value)
	if err != nil {
		panic(err)
	}
	return permission
}

func (permission Permission) String() string { return string(permission) }

func (permission Permission) Valid() bool {
	value := string(permission)
	if value == "*" {
		return true
	}
	if value == "" || len(value) > MaximumPermissionLength || value != strings.ToLower(value) {
		return false
	}
	segments := strings.Split(value, ".")
	for index, segment := range segments {
		if segment == "" {
			return false
		}
		if segment == "*" {
			return index == len(segments)-1
		}
		previousSeparator := false
		for charIndex := 0; charIndex < len(segment); charIndex++ {
			character := segment[charIndex]
			isLetter := character >= 'a' && character <= 'z'
			isDigit := character >= '0' && character <= '9'
			isSeparator := character == '_' || character == '-'
			if !isLetter && !isDigit && !isSeparator ||
				charIndex == 0 && isSeparator ||
				charIndex == len(segment)-1 && isSeparator ||
				isSeparator && previousSeparator {
				return false
			}
			previousSeparator = isSeparator
		}
	}
	return true
}

// PermissionSet is an immutable set of grants.
type PermissionSet struct {
	values map[Permission]struct{}
}

func NewPermissionSet(permissions ...Permission) (PermissionSet, error) {
	if len(permissions) > MaximumPermissions {
		return PermissionSet{}, newFault(ErrInvalidPermission, faults.CodeInvalidArgument, "invalid permission set", "too_many_permissions", "auth.NewPermissionSet", faults.Fields{"permission_count": len(permissions)})
	}
	set := PermissionSet{values: make(map[Permission]struct{}, len(permissions))}
	for _, permission := range permissions {
		if !permission.Valid() {
			return PermissionSet{}, newFault(
				ErrInvalidPermission,
				faults.CodeInvalidArgument,
				"invalid permission set",
				"invalid_permission",
				"auth.NewPermissionSet",
				faults.Fields{"permission": permission.String()},
			)
		}
		set.values[permission] = struct{}{}
	}
	return set, nil
}

func (set PermissionSet) IsZero() bool { return len(set.values) == 0 }
func (set PermissionSet) Len() int     { return len(set.values) }

func (set PermissionSet) Values() []Permission {
	values := make([]Permission, 0, len(set.values))
	for permission := range set.values {
		values = append(values, permission)
	}
	sort.Slice(values, func(left, right int) bool { return values[left] < values[right] })
	return values
}

func (set PermissionSet) Contains(permission Permission) bool {
	_, ok := set.values[permission]
	return ok
}

// Allows reports whether an exact or final-segment wildcard grant permits the
// requested non-wildcard permission.
func (set PermissionSet) Allows(requested Permission) bool {
	if !requested.Valid() || strings.Contains(requested.String(), "*") {
		return false
	}
	if set.Contains("*") || set.Contains(requested) {
		return true
	}
	segments := strings.Split(requested.String(), ".")
	for length := len(segments) - 1; length >= 1; length-- {
		candidate := Permission(strings.Join(segments[:length], ".") + ".*")
		if set.Contains(candidate) {
			return true
		}
	}
	return false
}

func (set PermissionSet) Merge(other PermissionSet) PermissionSet {
	merged := PermissionSet{values: make(map[Permission]struct{}, len(set.values)+len(other.values))}
	for permission := range set.values {
		merged.values[permission] = struct{}{}
	}
	for permission := range other.values {
		merged.values[permission] = struct{}{}
	}
	return merged
}

func (set PermissionSet) Validate() error {
	if len(set.values) > MaximumPermissions {
		return newFault(ErrInvalidPermission, faults.CodeInvalidArgument, "invalid permission set", "too_many_permissions", "auth.PermissionSet.Validate", faults.Fields{"permission_count": len(set.values)})
	}
	for permission := range set.values {
		if !permission.Valid() {
			return newFault(ErrInvalidPermission, faults.CodeInvalidArgument, "invalid permission set", "invalid_permission", "auth.PermissionSet.Validate", nil)
		}
	}
	return nil
}
