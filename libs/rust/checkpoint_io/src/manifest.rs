// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

//! Checkpoint manifest and deterministic encoding.

use mindclade_content_digest::Digest;
use mindclade_faults::{Code, Fault, FaultResult};
use mindclade_identifiers::ResourceId;
use mindclade_record_io::{Decoder, Encoder};
use std::collections::{BTreeMap, BTreeSet};

pub const CHECKPOINT_SCHEMA: u16 = 1;
const MAX_MANIFEST_BYTES: usize = 64 * 1024 * 1024;

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct CheckpointShard {
    pub name: String,
    pub digest: Digest,
    pub size: u64,
    pub rank: u32,
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct CheckpointManifest {
    pub checkpoint_id: ResourceId,
    pub run_id: ResourceId,
    pub step: u64,
    pub world_size: u32,
    pub parallel_plan: Digest,
    pub shards: Vec<CheckpointShard>,
    pub components: BTreeMap<String, Digest>,
}

impl CheckpointManifest {
    pub fn validate(&self) -> FaultResult<()> {
        if self.checkpoint_id.kind() != "checkpoint" || self.run_id.kind() != "run" {
            return Err(Fault::invalid_argument(
                "checkpoint or run ID kind is invalid",
            ));
        }
        if self.world_size == 0 || self.shards.is_empty() || self.shards.len() > 1_000_000 {
            return Err(Fault::invalid_argument(
                "checkpoint world size or shard count is invalid",
            ));
        }
        let mut names = BTreeSet::new();
        for shard in &self.shards {
            if shard.name.is_empty()
                || shard.name.len() > 512
                || shard.name.contains("..")
                || shard.name.starts_with('/')
            {
                return Err(Fault::invalid_argument("checkpoint shard name is invalid"));
            }
            if shard.rank >= self.world_size {
                return Err(Fault::invalid_argument(
                    "checkpoint shard rank exceeds world size",
                ));
            }
            if !names.insert(&shard.name) {
                return Err(Fault::new(
                    Code::AlreadyExists,
                    "checkpoint contains duplicate shard names",
                ));
            }
        }
        if self.components.len() > 4096 {
            return Err(Fault::new(
                Code::ResourceExhausted,
                "checkpoint component count exceeds limit",
            ));
        }
        for name in self.components.keys() {
            if !valid_component_name(name) {
                return Err(Fault::invalid_argument(
                    "checkpoint component name is invalid",
                ));
            }
        }
        Ok(())
    }
    pub fn encode(&self) -> FaultResult<Vec<u8>> {
        self.validate()?;
        let mut encoder = Encoder::new();
        encoder.u16(CHECKPOINT_SCHEMA);
        encoder.string(&self.checkpoint_id.to_string())?;
        encoder.string(&self.run_id.to_string())?;
        encoder.u64(self.step);
        encoder.u32(self.world_size);
        encoder.bytes(self.parallel_plan.as_bytes())?;
        encoder.u32(
            u32::try_from(self.shards.len())
                .map_err(|_| Fault::new(Code::OutOfRange, "checkpoint shard count exceeds u32"))?,
        );
        for shard in &self.shards {
            encoder.string(&shard.name)?;
            encoder.bytes(shard.digest.as_bytes())?;
            encoder.u64(shard.size);
            encoder.u32(shard.rank);
        }
        encoder.u32(
            u32::try_from(self.components.len()).map_err(|_| {
                Fault::new(Code::OutOfRange, "checkpoint component count exceeds u32")
            })?,
        );
        for (name, digest) in &self.components {
            encoder.string(name)?;
            encoder.bytes(digest.as_bytes())?;
        }
        let bytes = encoder.into_bytes();
        if bytes.len() > MAX_MANIFEST_BYTES {
            return Err(Fault::new(
                Code::ResourceExhausted,
                "checkpoint manifest exceeds size limit",
            ));
        }
        Ok(bytes)
    }
    pub fn decode(bytes: &[u8]) -> FaultResult<Self> {
        let mut decoder = Decoder::new(bytes, MAX_MANIFEST_BYTES)?;
        if decoder.u16()? != CHECKPOINT_SCHEMA {
            return Err(Fault::new(
                Code::FailedPrecondition,
                "checkpoint schema is unsupported",
            ));
        }
        let checkpoint_id = decoder
            .string()?
            .parse::<ResourceId>()
            .map_err(|error| Fault::data_loss("checkpoint ID is invalid").with_source(error))?;
        let run_id = decoder
            .string()?
            .parse::<ResourceId>()
            .map_err(|error| Fault::data_loss("run ID is invalid").with_source(error))?;
        let step = decoder.u64()?;
        let world_size = decoder.u32()?;
        let plan = <[u8; 32]>::try_from(decoder.bytes()?)
            .map_err(|_| Fault::data_loss("parallel-plan digest length is invalid"))?;
        let shard_count = decoder.item_count()?;
        let mut shards = Vec::with_capacity(shard_count);
        for _ in 0..shard_count {
            let name = decoder.string()?.to_owned();
            let digest = Digest::from_bytes(
                <[u8; 32]>::try_from(decoder.bytes()?)
                    .map_err(|_| Fault::data_loss("checkpoint shard digest length is invalid"))?,
            );
            let size = decoder.u64()?;
            let rank = decoder.u32()?;
            shards.push(CheckpointShard {
                name,
                digest,
                size,
                rank,
            });
        }
        let component_count = decoder.item_count()?;
        let mut components = BTreeMap::new();
        for _ in 0..component_count {
            let name = decoder.string()?.to_owned();
            let digest =
                Digest::from_bytes(<[u8; 32]>::try_from(decoder.bytes()?).map_err(|_| {
                    Fault::data_loss("checkpoint component digest length is invalid")
                })?);
            if components.insert(name, digest).is_some() {
                return Err(Fault::data_loss(
                    "checkpoint components contain duplicate names",
                ));
            }
        }
        decoder.finish()?;
        let manifest = Self {
            checkpoint_id,
            run_id,
            step,
            world_size,
            parallel_plan: Digest::from_bytes(plan),
            shards,
            components,
        };
        manifest.validate()?;
        Ok(manifest)
    }
}

fn valid_component_name(name: &str) -> bool {
    !name.is_empty()
        && name.len() <= 256
        && name.bytes().all(|byte| {
            byte.is_ascii_lowercase()
                || byte.is_ascii_digit()
                || matches!(byte, b'.' | b'-' | b'_' | b'/')
        })
        && !name.starts_with('/')
        && !name.ends_with('/')
        && !name.contains("//")
        && !name.split('/').any(|segment| matches!(segment, "." | ".."))
}
