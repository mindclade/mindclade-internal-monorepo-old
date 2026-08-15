// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package servicekit

import (
	"sort"
	"sync"

	"mindclade.internal/libs/go/faults"
)

// Stage defines the only supported production component-startup bands.
// Components start in ascending stage order and stop in the reverse order.
// Registration order is preserved within a stage.
type Stage uint8

const (
	// StageFoundation contains process-wide telemetry and other facilities that
	// must start first and stop last.
	StageFoundation Stage = iota + 1
	// StageInfrastructure contains durable stores, provider clients, caches,
	// migrations, and Kubernetes clients.
	StageInfrastructure
	// StageCoordination contains leases, outbox dispatchers, durable cursors,
	// work queues, and similar coordination mechanisms.
	StageCoordination
	// StageWork contains controllers, projectors, schedulers, coordinators, and
	// background processors.
	StageWork
	// StageServing contains externally reachable HTTP, Connect, and gRPC
	// transports. Starting these last prevents traffic from arriving before all
	// dependencies and workers are ready.
	StageServing
)

func (stage Stage) String() string {
	switch stage {
	case StageFoundation:
		return "foundation"
	case StageInfrastructure:
		return "infrastructure"
	case StageCoordination:
		return "coordination"
	case StageWork:
		return "work"
	case StageServing:
		return "serving"
	default:
		return "unknown"
	}
}

func (stage Stage) valid() bool {
	return stage >= StageFoundation && stage <= StageServing
}

// AssemblyEntry is a diagnostic snapshot of one staged component.
type AssemblyEntry struct {
	Stage     Stage
	Component string
}

type stagedComponent struct {
	stage     Stage
	sequence  uint64
	component Component
}

// Assembly is the standard production composition path for servicekit.
//
// Assembly deliberately does not construct databases, transports, Kubernetes
// managers, or domain engines. Existing adapters expose servicekit.Component
// values; Assembly only enforces the repository-wide lifecycle ordering law.
// It is safe for concurrent Add and Entries calls before Build. Build is
// single-use and freezes the assembly.
type Assembly struct {
	name    string
	options []Option

	mu       sync.Mutex
	entries  []stagedComponent
	names    map[string]struct{}
	sequence uint64
	built    bool
}

// NewAssembly creates an empty staged service composition.
func NewAssembly(name string, options ...Option) (*Assembly, error) {
	if err := validateName("service", name, "servicekit.NewAssembly"); err != nil {
		return nil, err
	}
	return &Assembly{
		name:    name,
		options: append([]Option(nil), options...),
		names:   make(map[string]struct{}),
	}, nil
}

// Add registers component in stage. A component name may appear only once.
func (assembly *Assembly) Add(stage Stage, component Component) error {
	if assembly == nil {
		return structuredFault(nil, ErrNilService, faults.CodeInvalidArgument, "service assembly must not be nil", "nil_assembly", "servicekit.Assembly.Add", nil)
	}
	if !stage.valid() {
		return structuredFault(nil, ErrInvalidName, faults.CodeInvalidArgument, "invalid service assembly stage", "invalid_assembly_stage", "servicekit.Assembly.Add", faults.Fields{"stage": stage.String()})
	}
	if err := component.validate(); err != nil {
		return err
	}

	assembly.mu.Lock()
	defer assembly.mu.Unlock()
	if assembly.built {
		return structuredFault(nil, ErrConfigurationFrozen, faults.CodeFailedPrecondition, "service assembly is frozen", "service_assembly_frozen", "servicekit.Assembly.Add", faults.Fields{FieldServiceName: assembly.name})
	}
	if _, exists := assembly.names[component.Name]; exists {
		return duplicateComponentError(assembly.name, component.Name)
	}
	assembly.sequence++
	assembly.entries = append(assembly.entries, stagedComponent{stage: stage, sequence: assembly.sequence, component: component})
	assembly.names[component.Name] = struct{}{}
	return nil
}

func (assembly *Assembly) AddFoundation(component Component) error {
	return assembly.Add(StageFoundation, component)
}
func (assembly *Assembly) AddInfrastructure(component Component) error {
	return assembly.Add(StageInfrastructure, component)
}
func (assembly *Assembly) AddCoordination(component Component) error {
	return assembly.Add(StageCoordination, component)
}
func (assembly *Assembly) AddWork(component Component) error {
	return assembly.Add(StageWork, component)
}
func (assembly *Assembly) AddServing(component Component) error {
	return assembly.Add(StageServing, component)
}

// Entries returns the deterministic order that Build will use.
func (assembly *Assembly) Entries() []AssemblyEntry {
	if assembly == nil {
		return nil
	}
	assembly.mu.Lock()
	entries := append([]stagedComponent(nil), assembly.entries...)
	assembly.mu.Unlock()
	sortStaged(entries)
	result := make([]AssemblyEntry, len(entries))
	for index, entry := range entries {
		result[index] = AssemblyEntry{Stage: entry.stage, Component: entry.component.Name}
	}
	return result
}

// Build freezes the assembly and creates the underlying Service. The returned
// Service remains single-use according to the normal servicekit contract.
func (assembly *Assembly) Build() (*Service, error) {
	if assembly == nil {
		return nil, structuredFault(nil, ErrNilService, faults.CodeInvalidArgument, "service assembly must not be nil", "nil_assembly", "servicekit.Assembly.Build", nil)
	}
	assembly.mu.Lock()
	if assembly.built {
		assembly.mu.Unlock()
		return nil, structuredFault(nil, ErrConfigurationFrozen, faults.CodeFailedPrecondition, "service assembly has already been built", "service_assembly_already_built", "servicekit.Assembly.Build", faults.Fields{FieldServiceName: assembly.name})
	}
	assembly.built = true
	entries := append([]stagedComponent(nil), assembly.entries...)
	options := append([]Option(nil), assembly.options...)
	name := assembly.name
	assembly.mu.Unlock()

	if len(entries) == 0 {
		return nil, structuredFault(nil, ErrNilComponent, faults.CodeFailedPrecondition, "service assembly contains no components", "empty_service_assembly", "servicekit.Assembly.Build", faults.Fields{FieldServiceName: name})
	}
	sortStaged(entries)
	service, err := New(name, options...)
	if err != nil {
		return nil, err
	}
	for _, entry := range entries {
		if err := service.Add(entry.component); err != nil {
			return nil, err
		}
	}
	return service, nil
}

func sortStaged(entries []stagedComponent) {
	sort.SliceStable(entries, func(left, right int) bool {
		if entries[left].stage == entries[right].stage {
			return entries[left].sequence < entries[right].sequence
		}
		return entries[left].stage < entries[right].stage
	})
}
