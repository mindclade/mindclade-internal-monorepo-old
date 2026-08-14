# RFC: <title>

- **Status:** Draft
- **Authors:** <owners>
- **Reviewers:** <required reviewers>
- **Created:** YYYY-MM-DD
- **Target decision:** YYYY-MM-DD
- **Related ADRs/issues:** <links>

## Summary

State the decision in one paragraph.

## Motivation and problem statement

Describe the concrete failure mode, scale, users, and why existing mechanisms
are insufficient. Separate observed evidence from assumptions.

## Goals and non-goals

List measurable outcomes and explicit exclusions.

## Current architecture

Document authoritative owners, data/control flows, contracts, persistence,
resource limits, failure behavior, and operational evidence.

## Proposed design

Cover APIs and schemas, dependency direction, language/process ownership,
state machines, idempotency, fencing, consistency, retries, cancellation,
drain, security, privacy, safety, observability, and compatibility.

## Alternatives

Include doing nothing and at least one materially different design. Explain the
tradeoff rather than presenting straw alternatives.

## Migration and rollback

Describe expand/contract steps, data conversion, dual-read/write period,
feature flags, rollback limits, and evidence needed at each gate.

## Qualification plan

List unit, conformance, integration, failure-injection, performance, scale,
security, and numerical tests with explicit pass criteria.

## Risks and open questions

Assign an owner and closure condition to each unresolved item.

## Decision

Completed by the approving authority. If accepted, create or supersede an ADR.
