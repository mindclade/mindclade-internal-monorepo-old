// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

//! Safe Rust core consumed by generated Python binding adapters.
#![forbid(unsafe_code)]
pub mod buffers;
pub mod errors;
pub mod manifests;
pub mod parsers;
pub mod tokenizers;
use mindclade_artifact_cas::ArtifactCas;
use mindclade_bytes_io::ByteSize;
use mindclade_content_digest::{Digest, hash_bytes};
use mindclade_data_stream::Cursor;
use mindclade_faults::{Fault, FaultResult};
use mindclade_ipc::Message;
use mindclade_manifests::ArtifactManifest;
use mindclade_tokenizer_runtime::{AlphabetTokenizer, Encoding, Tokenizer};

/// Stateless operations exposed by generated Python wrappers.
#[derive(Clone, Copy, Debug, Default)]
pub struct PythonBridge;

impl PythonBridge {
    #[must_use]
    pub fn sha256(value: &[u8]) -> String {
        hash_bytes(value).to_string()
    }
    pub fn validate_artifact_manifest(value: &[u8]) -> FaultResult<ArtifactManifest> {
        ArtifactManifest::decode(value)
    }
    pub fn protein_tokens(value: &[u8], maximum_tokens: usize) -> FaultResult<Encoding> {
        AlphabetTokenizer::protein()?.encode(value, maximum_tokens)
    }
    pub fn read_blob(cas: &ArtifactCas, digest: &str) -> FaultResult<Vec<u8>> {
        let digest = digest
            .parse::<Digest>()
            .map_err(|error| Fault::invalid_argument("digest is invalid").with_source(error))?;
        cas.get_blob(digest)
    }
    pub fn validate_cursor(value: &[u8]) -> FaultResult<Cursor> {
        Cursor::decode(value)
    }
    pub fn validate_ipc_message(value: &[u8], maximum_payload: usize) -> FaultResult<Message> {
        let maximum_payload = u64::try_from(maximum_payload)
            .map_err(|_| Fault::invalid_argument("maximum IPC payload exceeds u64"))?;
        Message::decode(value, ByteSize::new(maximum_payload))
    }
}
