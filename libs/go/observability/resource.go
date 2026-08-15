// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package observability

import (
	"strings"

	"mindclade.internal/libs/go/faults"
)

const maximumResourceValueLength = 256

// Resource describes the process emitting telemetry.
type Resource struct {
	serviceName       string
	serviceNamespace  string
	serviceVersion    string
	serviceInstanceID string
	environment       string
	region            string
	availabilityZone  string
	cluster           string
	node              string
	revision          string
	attributes        Attributes
}

type ResourceOption func(*Resource) error

func NewResource(serviceName string, options ...ResourceOption) (Resource, error) {
	resource := Resource{serviceName: strings.TrimSpace(serviceName)}
	for _, option := range options {
		if option != nil {
			if err := option(&resource); err != nil {
				return Resource{}, err
			}
		}
	}
	if err := resource.Validate(); err != nil {
		return Resource{}, err
	}
	return resource, nil
}

func WithServiceNamespace(value string) ResourceOption {
	return resourceStringOption("service.namespace", value, func(r *Resource, v string) { r.serviceNamespace = v })
}
func WithServiceVersion(value string) ResourceOption {
	return resourceStringOption("service.version", value, func(r *Resource, v string) { r.serviceVersion = v })
}
func WithServiceInstanceID(value string) ResourceOption {
	return resourceStringOption("service.instance.id", value, func(r *Resource, v string) { r.serviceInstanceID = v })
}
func WithDeploymentEnvironment(value string) ResourceOption {
	return resourceStringOption("deployment.environment.name", value, func(r *Resource, v string) { r.environment = v })
}
func WithCloudRegion(value string) ResourceOption {
	return resourceStringOption("cloud.region", value, func(r *Resource, v string) { r.region = v })
}
func WithCloudAvailabilityZone(value string) ResourceOption {
	return resourceStringOption("cloud.availability_zone", value, func(r *Resource, v string) { r.availabilityZone = v })
}
func WithKubernetesCluster(value string) ResourceOption {
	return resourceStringOption("k8s.cluster.name", value, func(r *Resource, v string) { r.cluster = v })
}
func WithKubernetesNode(value string) ResourceOption {
	return resourceStringOption("k8s.node.name", value, func(r *Resource, v string) { r.node = v })
}
func WithBuildRevision(value string) ResourceOption {
	return resourceStringOption("service.build.revision", value, func(r *Resource, v string) { r.revision = v })
}

func WithResourceAttributes(attributes Attributes) ResourceOption {
	return func(resource *Resource) error {
		resource.attributes = attributes
		return nil
	}
}

func resourceStringOption(name, value string, set func(*Resource, string)) ResourceOption {
	normalized := strings.TrimSpace(value)
	return func(resource *Resource) error {
		if normalized != "" && !validName(normalized, maximumResourceValueLength) {
			return invalidArgument(ErrInvalidResource, "invalid telemetry resource attribute", "invalid_resource_attribute", operationNewResource, faults.Fields{"attribute": name})
		}
		set(resource, normalized)
		return nil
	}
}

func (resource Resource) Validate() error {
	if !validName(resource.serviceName, maximumResourceValueLength) {
		return invalidArgument(ErrInvalidResource, "invalid telemetry service name", "invalid_service_name", operationNewResource, nil)
	}
	values := map[string]string{
		"service.namespace":           resource.serviceNamespace,
		"service.version":             resource.serviceVersion,
		"service.instance.id":         resource.serviceInstanceID,
		"deployment.environment.name": resource.environment,
		"cloud.region":                resource.region,
		"cloud.availability_zone":     resource.availabilityZone,
		"k8s.cluster.name":            resource.cluster,
		"k8s.node.name":               resource.node,
		"service.build.revision":      resource.revision,
	}
	for key, value := range values {
		if value != "" && !validName(value, maximumResourceValueLength) {
			return invalidArgument(ErrInvalidResource, "invalid telemetry resource attribute", "invalid_resource_attribute", operationNewResource, faults.Fields{"attribute": key})
		}
	}
	return nil
}

func (resource Resource) ServiceName() string       { return resource.serviceName }
func (resource Resource) ServiceNamespace() string  { return resource.serviceNamespace }
func (resource Resource) ServiceVersion() string    { return resource.serviceVersion }
func (resource Resource) ServiceInstanceID() string { return resource.serviceInstanceID }
func (resource Resource) Environment() string       { return resource.environment }
func (resource Resource) Region() string            { return resource.region }
func (resource Resource) AvailabilityZone() string  { return resource.availabilityZone }
func (resource Resource) Cluster() string           { return resource.cluster }
func (resource Resource) Node() string              { return resource.node }
func (resource Resource) Revision() string          { return resource.revision }

func (resource Resource) Attributes() Attributes {
	fields := faults.Fields{"service.name": resource.serviceName}
	optional := map[string]string{
		"service.namespace":           resource.serviceNamespace,
		"service.version":             resource.serviceVersion,
		"service.instance.id":         resource.serviceInstanceID,
		"deployment.environment.name": resource.environment,
		"cloud.region":                resource.region,
		"cloud.availability_zone":     resource.availabilityZone,
		"k8s.cluster.name":            resource.cluster,
		"k8s.node.name":               resource.node,
		"service.build.revision":      resource.revision,
	}
	for key, value := range optional {
		if value != "" {
			fields[key] = value
		}
	}
	base, _ := NewAttributes(fields)
	return resource.attributes.Merge(base)
}
