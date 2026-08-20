// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package production

import (
	"sort"
	"strings"

	"go.mindclade.dev/libs/go/faults"
	"go.mindclade.dev/libs/go/servicekit"
)

// Manifest records the mechanisms a concrete process composition actually
// wired. It is immutable after construction.
type Manifest struct {
	service      string
	role         Role
	capabilities map[Capability]struct{}
}

func NewManifest(service string, role Role, capabilities ...Capability) (Manifest, error) {
	service = strings.TrimSpace(service)
	if service == "" || !role.Valid() {
		return Manifest{}, invalid("invalid_production_manifest", service, role, nil)
	}
	values := make(map[Capability]struct{}, len(capabilities))
	for _, capability := range capabilities {
		if !capability.Valid() {
			return Manifest{}, invalid("invalid_production_capability", service, role, []string{capability.String()})
		}
		values[capability] = struct{}{}
	}
	manifest := Manifest{service: service, role: role, capabilities: values}
	if missing := manifest.Missing(); len(missing) != 0 {
		return Manifest{}, invalid("missing_production_capability", service, role, missing)
	}
	// Reuse servicekit's canonical service-name validation rather than creating
	// a second grammar in this leaf package.
	if _, err := servicekit.NewAssembly(service); err != nil {
		return Manifest{}, err
	}
	return manifest, nil
}

func (manifest Manifest) Service() string { return manifest.service }
func (manifest Manifest) Role() Role      { return manifest.role }

func (manifest Manifest) Has(capability Capability) bool {
	_, ok := manifest.capabilities[capability]
	return ok
}

func (manifest Manifest) Capabilities() []Capability {
	return sortedCapabilities(manifest.capabilities)
}

// Missing returns deterministic requirement names not satisfied by the
// manifest. It also reports an invalid role as a missing profile.
func (manifest Manifest) Missing() []string {
	if !manifest.role.Valid() {
		return []string{"valid_role"}
	}
	var missing []string
	for _, requirement := range Requirements(manifest.role) {
		satisfied := false
		for _, capability := range requirement.AnyOf {
			if manifest.Has(capability) {
				satisfied = true
				break
			}
		}
		if !satisfied {
			missing = append(missing, requirement.Name)
		}
	}
	sort.Strings(missing)
	return missing
}

// NewAssembly is the sole production entry into servicekit's staged lifecycle
// coordinator. Provider construction remains in the service composition root.
func (manifest Manifest) NewAssembly(options ...servicekit.Option) (*servicekit.Assembly, error) {
	if missing := manifest.Missing(); len(missing) != 0 || manifest.service == "" {
		return nil, invalid("invalid_production_manifest", manifest.service, manifest.role, missing)
	}
	return servicekit.NewAssembly(manifest.service, options...)
}

func invalid(reason, service string, role Role, missing []string) error {
	fields := faults.Fields{"service": service, "role": role.String()}
	if len(missing) != 0 {
		fields["missing"] = strings.Join(missing, ",")
	}
	return faults.New(
		faults.CodeFailedPrecondition,
		"Go process is missing required production mechanisms",
		faults.WithReason(reason),
		faults.WithOperation("servicekit.production.NewManifest"),
		faults.WithFields(fields),
		faults.WithRetryPolicy(faults.NoRetry()),
	)
}
