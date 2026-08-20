// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

//! Production detached-signature verification adapters.

use crate::{DetachedSignature, SignatureVerifier};
use ed25519_dalek::{Signature, Verifier, VerifyingKey};
use mindclade_faults::{Code, Fault, FaultResult};
use mindclade_runtime_core::{Clock, SystemClock};
use std::collections::BTreeMap;
use std::sync::Arc;
use std::time::UNIX_EPOCH;

const MAX_KEYS: usize = 1024;

#[derive(Clone, Debug)]
pub struct Ed25519VerificationKey {
    pub key_id: String,
    pub public_key: [u8; 32],
    pub not_before_unix_millis: u64,
    pub not_after_unix_millis: u64,
    pub disabled: bool,
}

impl Ed25519VerificationKey {
    pub fn validate(&self) -> FaultResult<()> {
        if self.key_id.is_empty()
            || self.key_id.len() > 256
            || self.not_before_unix_millis >= self.not_after_unix_millis
        {
            return Err(Fault::invalid_argument(
                "Ed25519 verification key metadata is invalid",
            ));
        }
        VerifyingKey::from_bytes(&self.public_key).map_err(|error| {
            Fault::invalid_argument("Ed25519 public key is invalid").with_source(error)
        })?;
        Ok(())
    }
}

#[derive(Clone)]
pub struct Ed25519KeySet {
    keys: BTreeMap<String, (VerifyingKey, u64, u64, bool)>,
    clock: Arc<dyn Clock>,
}

impl core::fmt::Debug for Ed25519KeySet {
    fn fmt(&self, formatter: &mut core::fmt::Formatter<'_>) -> core::fmt::Result {
        formatter
            .debug_struct("Ed25519KeySet")
            .field("key_count", &self.keys.len())
            .finish_non_exhaustive()
    }
}

impl Ed25519KeySet {
    pub fn new<I, K>(keys: I) -> FaultResult<Self>
    where
        I: IntoIterator<Item = (K, [u8; 32])>,
        K: Into<String>,
    {
        let keys = keys
            .into_iter()
            .map(|(key_id, public_key)| Ed25519VerificationKey {
                key_id: key_id.into(),
                public_key,
                not_before_unix_millis: 1,
                not_after_unix_millis: u64::MAX,
                disabled: false,
            });
        Self::with_clock(Arc::new(SystemClock), keys)
    }
    pub fn with_clock<I>(clock: Arc<dyn Clock>, keys: I) -> FaultResult<Self>
    where
        I: IntoIterator<Item = Ed25519VerificationKey>,
    {
        let mut output = BTreeMap::new();
        for key in keys {
            key.validate()?;
            if output.len() >= MAX_KEYS {
                return Err(Fault::new(
                    Code::ResourceExhausted,
                    "Ed25519 verification keyset exceeds bound",
                ));
            }
            let verifying = VerifyingKey::from_bytes(&key.public_key).map_err(|error| {
                Fault::invalid_argument("Ed25519 public key is invalid").with_source(error)
            })?;
            if output
                .insert(
                    key.key_id,
                    (
                        verifying,
                        key.not_before_unix_millis,
                        key.not_after_unix_millis,
                        key.disabled,
                    ),
                )
                .is_some()
            {
                return Err(Fault::new(
                    Code::AlreadyExists,
                    "duplicate Ed25519 verification key",
                ));
            }
        }
        if output.is_empty() {
            return Err(Fault::invalid_argument(
                "Ed25519 verifier requires at least one key",
            ));
        }
        Ok(Self {
            keys: output,
            clock,
        })
    }
    pub fn key_ids(&self) -> impl Iterator<Item = &str> {
        self.keys.keys().map(String::as_str)
    }
    fn now_unix_millis(&self) -> FaultResult<u64> {
        let elapsed = self
            .clock
            .system_now()
            .duration_since(UNIX_EPOCH)
            .map_err(|_| {
                Fault::new(
                    Code::FailedPrecondition,
                    "verification clock is before Unix epoch",
                )
            })?;
        u64::try_from(elapsed.as_millis()).map_err(|_| {
            Fault::new(
                Code::OutOfRange,
                "verification clock exceeds u64 milliseconds",
            )
        })
    }
}

impl SignatureVerifier for Ed25519KeySet {
    fn verify(&self, payload: &[u8], signature: &DetachedSignature) -> FaultResult<()> {
        signature.validate()?;
        if signature.algorithm != "ed25519" {
            return Err(Fault::new(
                Code::Unauthenticated,
                "signature algorithm is not accepted by Ed25519 verifier",
            ));
        }
        let (key, not_before, not_after, disabled) = self
            .keys
            .get(&signature.key_id)
            .ok_or_else(|| Fault::new(Code::Unauthenticated, "signature key is unknown"))?;
        let now = self.now_unix_millis()?;
        if *disabled || now < *not_before || now >= *not_after {
            return Err(Fault::new(
                Code::Unauthenticated,
                "signature key is inactive",
            ));
        }
        let signature = Signature::try_from(signature.value.as_slice()).map_err(|error| {
            Fault::new(Code::Unauthenticated, "Ed25519 signature length is invalid")
                .with_source(error)
        })?;
        key.verify(payload, &signature).map_err(|error| {
            Fault::new(
                Code::Unauthenticated,
                "Ed25519 signature verification failed",
            )
            .with_source(error)
        })
    }
}
