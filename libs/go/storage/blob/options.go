// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package blob

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"mindclade.internal/libs/go/identifiers"
)

const (
	DefaultListLimit = 100
	MaximumListLimit = 1000
)

type Preconditions struct {
	IfNotExists       bool
	IfGenerationMatch *int64
}

func (conditions Preconditions) Validate() error {
	if conditions.IfNotExists && conditions.IfGenerationMatch != nil {
		return invalidArgument(nil, ErrInvalidOptions, "invalid blob preconditions", "conflicting_blob_preconditions", "blob.Preconditions.Validate", nil)
	}
	if conditions.IfGenerationMatch != nil && *conditions.IfGenerationMatch <= 0 {
		return invalidArgument(nil, ErrInvalidOptions, "invalid blob preconditions", "invalid_blob_generation_precondition", "blob.Preconditions.Validate", nil)
	}
	return nil
}

type PutOptions struct {
	ContentType   string
	Metadata      Metadata
	Digest        identifiers.Digest
	Preconditions Preconditions
}

func (options PutOptions) Validate() error {
	if !validContentType(options.ContentType) {
		return invalidArgument(nil, ErrInvalidOptions, "invalid blob put options", "invalid_blob_content_type", "blob.PutOptions.Validate", nil)
	}
	if err := options.Metadata.Validate(); err != nil {
		return err
	}
	return options.Preconditions.Validate()
}

func validContentType(value string) bool {
	if len(value) > MaximumContentTypeLength || !utf8.ValidString(value) || strings.TrimSpace(value) != value {
		return false
	}
	for _, character := range value {
		if character == 0 || unicode.IsControl(character) {
			return false
		}
	}
	return true
}

type GetOptions struct {
	Offset     int64
	Length     int64
	Generation *int64
}

func (options GetOptions) Validate() error {
	if options.Offset < 0 || options.Length < 0 || options.Generation != nil && *options.Generation <= 0 {
		return invalidArgument(nil, ErrInvalidOptions, "invalid blob get options", "invalid_blob_get_options", "blob.GetOptions.Validate", nil)
	}
	return nil
}

type DeleteOptions struct{ Preconditions Preconditions }

func (options DeleteOptions) Validate() error {
	if options.Preconditions.IfNotExists {
		return invalidArgument(nil, ErrInvalidOptions, "invalid blob delete options", "delete_if_not_exists_unsupported", "blob.DeleteOptions.Validate", nil)
	}
	return options.Preconditions.Validate()
}

type ListOptions struct {
	Prefix string
	Cursor string
	Limit  int
}

func (options ListOptions) Normalized() (ListOptions, error) {
	if options.Limit == 0 {
		options.Limit = DefaultListLimit
	}
	if options.Limit < 0 || options.Limit > MaximumListLimit {
		return ListOptions{}, invalidListOptions("invalid_blob_list_limit")
	}
	if !validListPrefix(options.Prefix) {
		return ListOptions{}, invalidListOptions("invalid_blob_list_prefix")
	}
	if options.Cursor != "" {
		if _, err := ParseKey(options.Cursor); err != nil {
			return ListOptions{}, invalidArgument(nil, ErrInvalidOptions, "invalid blob list options", "invalid_blob_list_cursor", "blob.ListOptions.Normalized", nil)
		}
		if options.Prefix != "" && !strings.HasPrefix(options.Cursor, options.Prefix) {
			return ListOptions{}, invalidListOptions("blob_list_cursor_outside_prefix")
		}
	}
	return options, nil
}

func validListPrefix(value string) bool {
	if value == "" {
		return true
	}
	if len(value) > MaximumKeyLength || !utf8.ValidString(value) || strings.TrimSpace(value) != value {
		return false
	}
	if strings.HasPrefix(value, "/") || strings.Contains(value, "\\") || strings.Contains(value, "//") {
		return false
	}
	canonical := strings.TrimSuffix(value, "/")
	if canonical == "" {
		return false
	}
	for _, segment := range strings.Split(canonical, "/") {
		if segment == "" || segment == "." || segment == ".." {
			return false
		}
	}
	for _, character := range value {
		if character == 0 || unicode.IsControl(character) {
			return false
		}
	}
	return true
}

func invalidListOptions(reason string) error {
	return invalidArgument(nil, ErrInvalidOptions, "invalid blob list options", reason, "blob.ListOptions.Normalized", nil)
}
