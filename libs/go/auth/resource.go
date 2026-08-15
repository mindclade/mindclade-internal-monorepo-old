// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package auth

import (
	"errors"
	"strings"

	"mindclade.internal/libs/go/faults"
	"mindclade.internal/libs/go/identifiers"
)

type ResourceType string

func ParseResourceType(value string) (ResourceType, error) {
	normalized := strings.TrimSpace(value)
	resourceType := ResourceType(normalized)
	if !resourceType.Valid() {
		return "", newFault(ErrInvalidResource, faults.CodeInvalidArgument, "invalid resource type", "invalid_resource_type", "auth.ParseResourceType", faults.Fields{"resource_type": normalized})
	}
	return resourceType, nil
}

func (resourceType ResourceType) String() string { return string(resourceType) }
func (resourceType ResourceType) Valid() bool {
	value := string(resourceType)
	if value == "" || len(value) > 96 || value != strings.ToLower(value) {
		return false
	}
	previousSeparator := false
	for index := 0; index < len(value); index++ {
		character := value[index]
		isLetter := character >= 'a' && character <= 'z'
		isDigit := character >= '0' && character <= '9'
		isSeparator := character == '_' || character == '-' || character == '.'
		if !isLetter && !isDigit && !isSeparator ||
			index == 0 && !isLetter ||
			index == len(value)-1 && isSeparator ||
			isSeparator && previousSeparator {
			return false
		}
		previousSeparator = isSeparator
	}
	return true
}

type Resource struct {
	resourceType   ResourceType
	id             identifiers.ID
	organizationID identifiers.ID
	tenantID       identifiers.ID
	attributes     map[string]string
}

type ResourceOption func(*Resource) error

func WithResourceID(identifier identifiers.ID) ResourceOption {
	return func(resource *Resource) error { resource.id = identifier; return nil }
}
func WithResourceOrganizationID(identifier identifiers.ID) ResourceOption {
	return func(resource *Resource) error { resource.organizationID = identifier; return nil }
}
func WithResourceTenantID(identifier identifiers.ID) ResourceOption {
	return func(resource *Resource) error { resource.tenantID = identifier; return nil }
}
func WithResourceAttributes(attributes map[string]string) ResourceOption {
	captured := cloneAttributes(attributes)
	return func(resource *Resource) error {
		normalized, err := normalizeResourceAttributes(captured)
		if err != nil {
			return err
		}
		resource.attributes = normalized
		return nil
	}
}

func NewResource(resourceType ResourceType, options ...ResourceOption) (Resource, error) {
	resource := Resource{resourceType: resourceType}
	for _, option := range options {
		if option != nil {
			if err := option(&resource); err != nil {
				return Resource{}, err
			}
		}
	}
	resource.attributes = cloneAttributes(resource.attributes)
	if err := resource.Validate(); err != nil {
		return Resource{}, err
	}
	return resource, nil
}

func (resource Resource) Type() ResourceType             { return resource.resourceType }
func (resource Resource) ID() identifiers.ID             { return resource.id }
func (resource Resource) OrganizationID() identifiers.ID { return resource.organizationID }
func (resource Resource) TenantID() identifiers.ID       { return resource.tenantID }
func (resource Resource) Attributes() map[string]string  { return cloneAttributes(resource.attributes) }

func (resource Resource) Validate() error {
	if !resource.resourceType.Valid() {
		return newFault(ErrInvalidResource, faults.CodeInvalidArgument, "invalid authorization resource", "invalid_resource_type", "auth.Resource.Validate", nil)
	}
	for _, identifier := range []identifiers.ID{resource.id, resource.organizationID, resource.tenantID} {
		if !identifier.IsZero() {
			if err := identifier.Validate(); err != nil {
				return newFault(errors.Join(ErrInvalidResource, err), faults.CodeInvalidArgument, "invalid authorization resource", "invalid_resource_identifier", "auth.Resource.Validate", nil)
			}
		}
	}
	if _, err := normalizeResourceAttributes(resource.attributes); err != nil {
		return err
	}
	return nil
}
