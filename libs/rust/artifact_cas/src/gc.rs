//! Deterministic, version-conditional artifact garbage collection.
//!
//! Eligibility belongs to the Go control plane. The Rust byte plane executes
//! only an immutable object-path/version plan, recomputes its plan digest, and
//! reports an outcome for every candidate. Changed objects are never deleted by
//! a stale plan.

use mindclade_content_digest::{hash_bytes, Digest};
use mindclade_faults::{Code, Fault, FaultResult};
use mindclade_object_store::{ObjectPath, ObjectStore};
use mindclade_runtime_core::ResourceVersion;
use std::collections::BTreeSet;

const MAXIMUM_CANDIDATES: usize = 1_000_000;

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct GarbageCollectionCandidate {
    pub digest: Digest,
    pub path: ObjectPath,
    pub expected_version: ResourceVersion,
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct GarbageCollectionPlan {
    pub plan_digest: Digest,
    pub candidates: Vec<GarbageCollectionCandidate>,
}

impl GarbageCollectionPlan {
    /// Builds a canonical plan and derives the same digest used by the Go
    /// control-plane planner.
    pub fn build(
        candidates: impl IntoIterator<Item = GarbageCollectionCandidate>,
    ) -> FaultResult<Self> {
        let candidates = canonical_candidates(candidates)?;
        let plan_digest = compute_plan_digest(&candidates)?;
        Ok(Self {
            plan_digest,
            candidates,
        })
    }

    /// Accepts a control-plane plan only if the supplied digest matches the
    /// canonical Rust recomputation. This prevents candidate substitution or
    /// path/version reconstruction in the byte plane.
    pub fn from_control_plane(
        plan_digest: Digest,
        candidates: impl IntoIterator<Item = GarbageCollectionCandidate>,
    ) -> FaultResult<Self> {
        let candidates = canonical_candidates(candidates)?;
        let actual = compute_plan_digest(&candidates)?;
        if actual != plan_digest {
            return Err(Fault::new(
                Code::FailedPrecondition,
                "garbage-collection plan digest mismatch",
            ));
        }
        Ok(Self {
            plan_digest,
            candidates,
        })
    }

    #[must_use]
    pub fn is_empty(&self) -> bool {
        self.candidates.is_empty()
    }
}

fn canonical_candidates(
    candidates: impl IntoIterator<Item = GarbageCollectionCandidate>,
) -> FaultResult<Vec<GarbageCollectionCandidate>> {
    let mut seen = BTreeSet::new();
    let mut canonical = Vec::new();
    for candidate in candidates {
        let key = (candidate.digest, candidate.path.as_str().to_owned());
        if !seen.insert(key) {
            return Err(Fault::new(
                Code::AlreadyExists,
                "garbage-collection plan contains duplicate candidate",
            ));
        }
        canonical.push(candidate);
        if canonical.len() > MAXIMUM_CANDIDATES {
            return Err(Fault::new(
                Code::ResourceExhausted,
                "garbage-collection plan exceeds candidate limit",
            ));
        }
    }
    canonical.sort_by(|a, b| {
        a.digest
            .cmp(&b.digest)
            .then_with(|| a.path.as_str().cmp(b.path.as_str()))
    });
    Ok(canonical)
}

fn compute_plan_digest(candidates: &[GarbageCollectionCandidate]) -> FaultResult<Digest> {
    let mut payload = Vec::new();
    for candidate in candidates {
        // Go canonical form:
        // digest + NUL + object_path + NUL + resource_version + newline.
        payload.extend_from_slice(candidate.digest.to_string().as_bytes());
        payload.push(0);
        payload.extend_from_slice(candidate.path.as_str().as_bytes());
        payload.push(0);
        payload.extend_from_slice(candidate.expected_version.to_string().as_bytes());
        payload.push(b'\n');
    }
    Ok(hash_bytes(&payload))
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum SweepOutcome {
    Deleted,
    AlreadyAbsent,
    Stale,
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct SweepResult {
    pub digest: Digest,
    pub path: ObjectPath,
    pub outcome: SweepOutcome,
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct SweepReport {
    pub plan_digest: Digest,
    pub results: Vec<SweepResult>,
    pub deleted: usize,
    pub already_absent: usize,
    pub stale: usize,
}

impl SweepReport {
    fn new(plan_digest: Digest, capacity: usize) -> Self {
        Self {
            plan_digest,
            results: Vec::with_capacity(capacity),
            deleted: 0,
            already_absent: 0,
            stale: 0,
        }
    }
}

pub fn sweep(store: &dyn ObjectStore, plan: &GarbageCollectionPlan) -> FaultResult<SweepReport> {
    let expected = compute_plan_digest(&plan.candidates)?;
    if expected != plan.plan_digest {
        return Err(Fault::new(
            Code::FailedPrecondition,
            "garbage-collection plan changed after validation",
        ));
    }

    let mut report = SweepReport::new(plan.plan_digest, plan.candidates.len());
    for candidate in &plan.candidates {
        let current = store.head(&candidate.path)?;
        let outcome = if let Some(meta) = current {
            if meta.digest != candidate.digest || meta.version != candidate.expected_version {
                SweepOutcome::Stale
            } else {
                match store.delete(&candidate.path, Some(candidate.expected_version)) {
                    Ok(true) => SweepOutcome::Deleted,
                    Ok(false) => SweepOutcome::AlreadyAbsent,
                    Err(error) if error.code() == Code::Conflict => SweepOutcome::Stale,
                    Err(error) => return Err(error),
                }
            }
        } else {
            SweepOutcome::AlreadyAbsent
        };
        match outcome {
            SweepOutcome::Deleted => report.deleted += 1,
            SweepOutcome::AlreadyAbsent => report.already_absent += 1,
            SweepOutcome::Stale => report.stale += 1,
        }
        report.results.push(SweepResult {
            digest: candidate.digest,
            path: candidate.path.clone(),
            outcome,
        });
    }
    Ok(report)
}
