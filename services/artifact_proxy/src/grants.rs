// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

//! Local artifact authorization derived from a signed execution ticket.

use crate::AccessContext;
use mindclade_content_digest::Digest;
use mindclade_faults::{Code, Fault, FaultResult};
use mindclade_worker_protocol::{ArtifactGrant, ExecutionTicket, RevocationSnapshot, SignatureVerifier};

#[derive(Clone, Debug)]
pub struct ValidatedGrant {
    pub access: AccessContext,
    pub grant: ArtifactGrant,
    pub ticket_id: String,
    pub expires_unix_millis: u64,
}

impl ValidatedGrant {
    pub fn from_ticket<V: SignatureVerifier + ?Sized>(
        ticket: &ExecutionTicket,
        principal_id: impl Into<String>,
        now_unix_millis: u64,
        minimum_policy_epoch: u64,
        minimum_route_version: u64,
        revocations: &RevocationSnapshot,
        verifier: &V,
    ) -> FaultResult<Self> {
        ticket.validate(
            now_unix_millis,
            minimum_policy_epoch,
            minimum_route_version,
            revocations,
            verifier,
        )?;
        let access = AccessContext {
            tenant_id: ticket.claims.tenant_id.clone(),
            workspace_id: ticket.claims.workspace_id.clone(),
            principal_id: principal_id.into(),
        };
        access.validate()?;
        ticket.claims.artifacts.validate()?;
        Ok(Self {
            access,
            grant: ticket.claims.artifacts.clone(),
            ticket_id: ticket.claims.ticket_id.to_string(),
            expires_unix_millis: ticket.claims.expires_unix_millis,
        })
    }

    /// Re-check the time bound for every byte-plane operation.
    ///
    /// A `ValidatedGrant` may be cached for a short period, but validation at
    /// construction time never turns an expiring ticket into timeless local
    /// authority.
    pub fn require_active(&self, now_unix_millis: u64) -> FaultResult<()> {
        if now_unix_millis >= self.expires_unix_millis {
            return Err(Fault::new(
                Code::DeadlineExceeded,
                "artifact grant has expired",
            ));
        }
        Ok(())
    }

    /// Authorize a digest before any provider/cache access occurs.
    pub fn authorize_read(&self, digest: Digest, now_unix_millis: u64) -> FaultResult<()> {
        self.require_active(now_unix_millis)?;
        if !self.grant.readable_digests.contains(&digest) {
            return Err(Fault::new(
                Code::PermissionDenied,
                "artifact digest is outside the read grant",
            ));
        }
        Ok(())
    }

    pub fn require_read(
        &self,
        digest: Digest,
        bytes: u64,
        now_unix_millis: u64,
    ) -> FaultResult<()> {
        self.authorize_read(digest, now_unix_millis)?;
        if bytes > self.grant.maximum_read_bytes {
            return Err(Fault::new(
                Code::ResourceExhausted,
                "artifact read exceeds grant budget",
            ));
        }
        Ok(())
    }

    pub fn require_range(
        &self,
        digest: Digest,
        bytes: u64,
        now_unix_millis: u64,
    ) -> FaultResult<()> {
        if !self.grant.allow_range_reads {
            return Err(Fault::new(
                Code::PermissionDenied,
                "range reads are not allowed by grant",
            ));
        }
        self.require_read(digest, bytes, now_unix_millis)
    }

    pub fn require_write(
        &self,
        namespace: &str,
        bytes: u64,
        now_unix_millis: u64,
    ) -> FaultResult<()> {
        self.require_active(now_unix_millis)?;
        if !self.grant.writable_namespaces.contains(namespace) {
            return Err(Fault::new(
                Code::PermissionDenied,
                "artifact namespace is outside the write grant",
            ));
        }
        if bytes > self.grant.maximum_write_bytes {
            return Err(Fault::new(
                Code::ResourceExhausted,
                "artifact write exceeds grant budget",
            ));
        }
        Ok(())
    }
}
