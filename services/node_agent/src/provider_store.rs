// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

//! Provider-backed immutable artifact/reference source used by node-local staging.
//!
//! This adapter intentionally exposes no arbitrary relative-path read API.
//! Every access is tied either to the ticket's explicit artifact grant or to
//! its immutable reference-database snapshot identity.

use bytes::Bytes;
use mindclade_content_digest::{Digest, hash_bytes};
use mindclade_faults::{Code, Fault, FaultResult};
use mindclade_object_store::adapters::arrow::ArrowProvider;
use mindclade_worker_protocol::ExecutionTicket;

#[derive(Clone, Debug)]
pub struct ProviderSource {
    provider: ArrowProvider,
}

impl ProviderSource {
    #[must_use]
    pub fn new(provider: ArrowProvider) -> Self {
        Self { provider }
    }

    pub async fn read_artifact(
        &self,
        ticket: &ExecutionTicket,
        digest: Digest,
        now_unix_millis: u64,
    ) -> FaultResult<Bytes> {
        require_ticket_active(ticket, now_unix_millis)?;
        authorize_read(ticket, digest)?;
        let bytes = self
            .provider
            .get(&artifact_path(digest), Some(digest))
            .await?;
        let length = byte_len(bytes.len())?;
        if length > ticket.claims.artifacts.maximum_read_bytes {
            return Err(Fault::new(
                Code::ResourceExhausted,
                "artifact read exceeds execution-ticket byte budget",
            ));
        }
        Ok(bytes)
    }

    pub async fn read_artifact_range(
        &self,
        ticket: &ExecutionTicket,
        digest: Digest,
        start: u64,
        length: u64,
        now_unix_millis: u64,
    ) -> FaultResult<Bytes> {
        require_ticket_active(ticket, now_unix_millis)?;
        authorize_read(ticket, digest)?;
        if !ticket.claims.artifacts.allow_range_reads {
            return Err(Fault::new(
                Code::PermissionDenied,
                "artifact range reads are not allowed by execution ticket",
            ));
        }
        if length == 0 || length > ticket.claims.artifacts.maximum_read_bytes {
            return Err(Fault::new(
                Code::ResourceExhausted,
                "artifact range exceeds execution-ticket byte budget",
            ));
        }
        self.provider
            .get_range(&artifact_path(digest), start, length)
            .await
    }

    pub async fn publish_artifact(
        &self,
        ticket: &ExecutionTicket,
        namespace: &str,
        bytes: Bytes,
        now_unix_millis: u64,
    ) -> FaultResult<Digest> {
        require_ticket_active(ticket, now_unix_millis)?;
        let length = byte_len(bytes.len())?;
        authorize_write(ticket, namespace, length)?;
        if bytes.is_empty() {
            return Err(Fault::invalid_argument("artifact content is empty"));
        }
        let digest = hash_bytes(&bytes);
        match self
            .provider
            .put_create(&artifact_path(digest), bytes)
            .await
        {
            Ok(_) => Ok(digest),
            Err(error) if error.code() == Code::AlreadyExists => {
                self.provider
                    .get(&artifact_path(digest), Some(digest))
                    .await?;
                Ok(digest)
            }
            Err(error) => Err(error),
        }
    }

    /// Read the immutable reference-snapshot manifest named by the ticket.
    pub async fn read_reference_snapshot(
        &self,
        ticket: &ExecutionTicket,
        digest: Digest,
        now_unix_millis: u64,
    ) -> FaultResult<Bytes> {
        require_ticket_active(ticket, now_unix_millis)?;
        if ticket.claims.reference_snapshot != Some(digest) {
            return Err(Fault::new(
                Code::PermissionDenied,
                "reference snapshot does not match execution ticket",
            ));
        }
        self.provider
            .get(&reference_path(digest), Some(digest))
            .await
    }
}

fn require_ticket_active(ticket: &ExecutionTicket, now_unix_millis: u64) -> FaultResult<()> {
    if now_unix_millis < ticket.claims.not_before_unix_millis
        || now_unix_millis >= ticket.claims.expires_unix_millis
        || now_unix_millis > ticket.claims.deadline_unix_millis
    {
        return Err(Fault::new(
            Code::DeadlineExceeded,
            "execution ticket is not active for provider access",
        ));
    }
    Ok(())
}

fn authorize_read(ticket: &ExecutionTicket, digest: Digest) -> FaultResult<()> {
    if !ticket.claims.artifacts.readable_digests.contains(&digest) {
        return Err(Fault::new(
            Code::PermissionDenied,
            "artifact digest is outside the execution-ticket grant",
        ));
    }
    Ok(())
}

fn authorize_write(ticket: &ExecutionTicket, namespace: &str, length: u64) -> FaultResult<()> {
    if !ticket
        .claims
        .artifacts
        .writable_namespaces
        .contains(namespace)
    {
        return Err(Fault::new(
            Code::PermissionDenied,
            "artifact namespace is outside the execution-ticket grant",
        ));
    }
    if length > ticket.claims.artifacts.maximum_write_bytes {
        return Err(Fault::new(
            Code::ResourceExhausted,
            "artifact write exceeds execution-ticket byte budget",
        ));
    }
    Ok(())
}

fn artifact_path(digest: Digest) -> String {
    let text = digest.to_string();
    let hex = text.strip_prefix("sha256:").unwrap_or(&text);
    format!("sha256/{}/{hex}", &hex[..2])
}

fn reference_path(digest: Digest) -> String {
    let text = digest.to_string();
    let hex = text.strip_prefix("sha256:").unwrap_or(&text);
    format!("references/sha256/{}/{hex}", &hex[..2])
}

fn byte_len(value: usize) -> FaultResult<u64> {
    u64::try_from(value)
        .map_err(|_| Fault::new(Code::OutOfRange, "provider byte count exceeds u64"))
}
