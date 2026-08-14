//! Canonical local ticket/grant validation facade over worker_protocol.

pub use mindclade_worker_protocol::{
    AdmissionGrant, ArtifactGrant, ExecutionBudget, ExecutionTicket, RevocationSnapshot,
    RouteSnapshot, SignatureVerifier,
};
