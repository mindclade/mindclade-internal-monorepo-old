// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

//! In-memory artifact-ID to manifest-digest index used by tests and local tooling.

use mindclade_content_digest::Digest;
use mindclade_faults::{Code, Fault, FaultResult};
use mindclade_identifiers::ResourceId;
use std::collections::BTreeMap;
use std::collections::btree_map::Entry;

#[derive(Clone, Debug, Default)]
pub struct CasIndex {
    manifests: BTreeMap<ResourceId, Digest>,
}

impl CasIndex {
    /// Insert an artifact mapping without ever mutating an existing mapping.
    pub fn insert(&mut self, id: ResourceId, digest: Digest) -> FaultResult<()> {
        if id.kind() != "artifact" {
            return Err(Fault::invalid_argument(
                "CAS index keys must use artifact resource IDs",
            ));
        }
        match self.manifests.entry(id) {
            Entry::Vacant(entry) => {
                entry.insert(digest);
                Ok(())
            }
            Entry::Occupied(entry) if *entry.get() == digest => Ok(()),
            Entry::Occupied(entry) => Err(Fault::new(
                Code::Conflict,
                "artifact ID is already bound to a different manifest digest",
            )
            .with_context("existing_digest", entry.get().to_string())
            .with_context("candidate_digest", digest.to_string())),
        }
    }
    #[must_use]
    pub fn get(&self, id: &ResourceId) -> Option<Digest> {
        self.manifests.get(id).copied()
    }
    pub fn remove(&mut self, id: &ResourceId, expected: Digest) -> FaultResult<bool> {
        match self.manifests.get(id).copied() {
            None => Ok(false),
            Some(current) if current != expected => Err(Fault::new(
                Code::Conflict,
                "artifact index changed before removal",
            )
            .with_context("existing_digest", current.to_string())
            .with_context("expected_digest", expected.to_string())),
            Some(_) => {
                self.manifests.remove(id);
                Ok(true)
            }
        }
    }
    #[must_use]
    pub fn len(&self) -> usize {
        self.manifests.len()
    }
    #[must_use]
    pub fn is_empty(&self) -> bool {
        self.manifests.is_empty()
    }
}
