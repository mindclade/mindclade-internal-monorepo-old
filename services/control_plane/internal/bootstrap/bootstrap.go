// Copyright 2026 Mindclade. All rights reserved.
// Confidential, proprietary, and trade-secret information.

package bootstrap

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"

	"mindclade.internal/libs/go/faults"
)

const (
	ExitSuccess       = 0
	ExitFailure       = 1
	ExitInvalidUsage  = 2
	ExitNotConfigured = 78
)

// Main is the common command entrypoint. Concrete commands pass a role and a
// service-owned factory; signal handling and lifecycle behavior remain shared.
func Main(role Role, factory Factory) {
	os.Exit(RunCommand(context.Background(), role, factory, os.Args[1:], os.Stdout, os.Stderr))
}

// RunCommand executes the standard command contract and is separated from Main
// for deterministic tests. --describe-profile is intentionally available
// before deployment adapters are materialized.
func RunCommand(ctx context.Context, role Role, factory Factory, args []string, stdout, stderr io.Writer) int {
	if stdout == nil {
		stdout = io.Discard
	}
	if stderr == nil {
		stderr = io.Discard
	}
	if len(args) == 1 && args[0] == "--describe-profile" {
		if err := writeDescription(stdout, role); err != nil {
			writeCommandError(stderr, role, err)
			return ExitFailure
		}
		return ExitSuccess
	}
	if len(args) != 0 {
		_, _ = fmt.Fprintln(stderr, "usage: command [--describe-profile]")
		return ExitInvalidUsage
	}
	if factory == nil {
		writeCommandError(stderr, role, faults.New(
			faults.CodeFailedPrecondition,
			"control-plane production adapters are not configured",
			faults.WithReason("nil_production_factory"),
			faults.WithOperation("controlplane.bootstrap.RunCommand"),
			faults.WithRetryPolicy(faults.NoRetry()),
		))
		return ExitNotConfigured
	}
	if err := Execute(ctx, role, factory); err != nil {
		writeCommandError(stderr, role, err)
		if faults.ReasonOf(err) == "production_factory_not_materialized" {
			return ExitNotConfigured
		}
		return ExitFailure
	}
	return ExitSuccess
}

// UnconfiguredFactory is used only by scaffold composition roots. It fails
// closed so a placeholder binary cannot be promoted accidentally. Deployment
// work replaces it with a service-owned adapter factory without changing the
// command lifecycle path.
func UnconfiguredFactory(command string) Factory {
	return FactoryFunc(func(context.Context, Profile) (Runtime, error) {
		return Runtime{}, faults.New(
			faults.CodeFailedPrecondition,
			"control-plane production adapters are not configured",
			faults.WithReason("production_factory_not_materialized"),
			faults.WithOperation("controlplane.bootstrap.UnconfiguredFactory"),
			faults.WithField("command", command),
			faults.WithRetryPolicy(faults.NoRetry()),
		)
	})
}

type commandDescription struct {
	Role           string              `json:"role"`
	Service        string              `json:"service"`
	ProductionRole string              `json:"production_role"`
	Requirements   []requirementRecord `json:"requirements"`
	Packages       []string            `json:"foundation_packages"`
}

type requirementRecord struct {
	Name  string   `json:"name"`
	AnyOf []string `json:"any_of"`
}

func writeDescription(writer io.Writer, role Role) error {
	profile, err := ProfileFor(role)
	if err != nil {
		return err
	}
	consumption, err := ConsumptionFor(role)
	if err != nil {
		return err
	}
	requirements := profile.Requirements()
	records := make([]requirementRecord, 0, len(requirements))
	for _, requirement := range requirements {
		values := make([]string, len(requirement.AnyOf))
		for index, capability := range requirement.AnyOf {
			values[index] = capability.String()
		}
		records = append(records, requirementRecord{Name: requirement.Name, AnyOf: values})
	}
	encoder := json.NewEncoder(writer)
	encoder.SetIndent("", "  ")
	return encoder.Encode(commandDescription{
		Role:           role.String(),
		Service:        profile.Name,
		ProductionRole: profile.ProductionRole.String(),
		Requirements:   records,
		Packages:       consumption.Packages,
	})
}

func writeCommandError(writer io.Writer, role Role, err error) {
	_, _ = fmt.Fprintf(writer, "%s: %s (%s)\n", role.String(), faults.PublicMessageOf(err), faults.ReasonOf(err))
}
