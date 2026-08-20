// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

//! Atomic local-filesystem provider.

use crate::{ObjectMeta, ObjectPath, ObjectStore, PutCondition, PutResult};
use mindclade_atomic_fs::{AtomicFileStore, RelativePath};
use mindclade_bytes_io::{ByteRange, ByteSize};
use mindclade_content_digest::{Digest, Sha256, hash_bytes};
use mindclade_faults::{Code, Fault, FaultResult};
use mindclade_runtime_core::ResourceVersion;
use std::fs;
use std::io::{Read, Seek, SeekFrom};
use std::path::{Path, PathBuf};
use std::sync::RwLock;
use std::time::{Duration, SystemTime, UNIX_EPOCH};

#[derive(Debug)]
pub struct LocalStore {
    files: AtomicFileStore,
    metadata_root: PathBuf,
    metadata_files: AtomicFileStore,
    operations: RwLock<()>,
}

const METADATA_DIRECTORY: &str = ".mindclade-object-metadata";
const MAXIMUM_LIST_CANDIDATES: usize = 1_000_000;
const METADATA_V2_HEADER: &str = "mindclade-object-meta-v2";
const VERIFICATION_CHUNK_BYTES: u64 = 4 * 1024 * 1024;
const MAXIMUM_METADATA_BYTES: u64 = 64 * 1024 * 1024;

#[derive(Debug)]
struct LocalMetadata {
    meta: ObjectMeta,
    chunks: Option<Vec<Digest>>,
}

impl LocalStore {
    pub fn new(root: impl Into<PathBuf>) -> FaultResult<Self> {
        let root = root.into();
        let files = AtomicFileStore::new(&root)?;
        let metadata_root = root.join(METADATA_DIRECTORY);
        fs::create_dir_all(&metadata_root).map_err(|error| {
            Fault::internal("failed to create object metadata directory").with_source(error)
        })?;
        let metadata_files = AtomicFileStore::new(&metadata_root)?;
        Ok(Self {
            files,
            metadata_root,
            metadata_files,
            operations: RwLock::new(()),
        })
    }
    fn relative(path: &ObjectPath) -> FaultResult<RelativePath> {
        if path
            .as_str()
            .split('/')
            .next()
            .is_some_and(|component| component == METADATA_DIRECTORY)
        {
            return Err(Fault::invalid_argument(
                "object path uses the reserved metadata namespace",
            ));
        }
        RelativePath::new(path.as_str()).map_err(|error| Fault::invalid_argument(error.to_string()))
    }
    fn absolute(&self, path: &ObjectPath) -> PathBuf {
        self.files.root().join(path.as_str())
    }
    fn metadata_path(&self, path: &ObjectPath) -> PathBuf {
        let digest = hash_bytes(path.as_str().as_bytes()).to_hex();
        self.metadata_root.join(format!("{digest}.meta"))
    }
    fn read_metadata(&self, path: &ObjectPath) -> FaultResult<Option<LocalMetadata>> {
        let absolute = self.absolute(path);
        let Some(file_metadata) = object_file_metadata(&absolute)? else {
            return Ok(None);
        };
        let bytes = read_bounded_metadata(&self.metadata_path(path))?;
        let text = std::str::from_utf8(&bytes)
            .map_err(|error| Fault::data_loss("object metadata is not UTF-8").with_source(error))?;
        let mut lines = text.lines();
        let first = lines
            .next()
            .ok_or_else(|| Fault::data_loss("object metadata header is missing"))?;
        let version_two = first == METADATA_V2_HEADER;
        let generation_text = if version_two {
            lines
                .next()
                .ok_or_else(|| Fault::data_loss("object metadata generation is missing"))?
        } else {
            first
        };
        let generation = generation_text.parse::<u64>().map_err(|error| {
            Fault::data_loss("object metadata generation is invalid").with_source(error)
        })?;
        let digest = lines
            .next()
            .ok_or_else(|| Fault::data_loss("object metadata digest is missing"))?
            .parse::<Digest>()
            .map_err(|error| {
                Fault::data_loss("object metadata digest is invalid").with_source(error)
            })?;
        let modified_millis = lines
            .next()
            .ok_or_else(|| Fault::data_loss("object metadata timestamp is missing"))?
            .parse::<u64>()
            .map_err(|error| {
                Fault::data_loss("object metadata timestamp is invalid").with_source(error)
            })?;
        let size = if version_two {
            let declared = parse_metadata_u64(&mut lines, "object metadata size")?;
            if declared != file_metadata.len() {
                return Err(Fault::data_loss(
                    "object size does not match its chunk manifest",
                ));
            }
            declared
        } else {
            file_metadata.len()
        };
        let chunks = if version_two {
            let chunk_bytes = parse_metadata_u64(&mut lines, "object metadata chunk size")?;
            if chunk_bytes != VERIFICATION_CHUNK_BYTES {
                return Err(Fault::data_loss(
                    "object metadata chunk size is unsupported",
                ));
            }
            let count = parse_metadata_u64(&mut lines, "object metadata chunk count")?;
            let expected = chunk_count(size)?;
            if count != expected {
                return Err(Fault::data_loss(
                    "object metadata chunk count does not match object size",
                ));
            }
            let count = usize::try_from(count).map_err(|_| {
                Fault::new(
                    Code::ResourceExhausted,
                    "object chunk manifest exceeds platform limits",
                )
            })?;
            let mut chunks = Vec::with_capacity(count);
            for _ in 0..count {
                let digest = lines
                    .next()
                    .ok_or_else(|| Fault::data_loss("object chunk digest is missing"))?
                    .parse::<Digest>()
                    .map_err(|error| {
                        Fault::data_loss("object chunk digest is invalid").with_source(error)
                    })?;
                chunks.push(digest);
            }
            if lines.next().is_some() {
                return Err(Fault::data_loss(
                    "object metadata contains unexpected trailing fields",
                ));
            }
            Some(chunks)
        } else {
            None
        };
        Ok(Some(LocalMetadata {
            meta: ObjectMeta {
                path: path.clone(),
                size: ByteSize::new(size),
                digest,
                version: ResourceVersion::new(generation, digest),
                modified: UNIX_EPOCH + Duration::from_millis(modified_millis),
            },
            chunks,
        }))
    }
    fn read_meta(&self, path: &ObjectPath) -> FaultResult<Option<ObjectMeta>> {
        self.read_metadata(path)
            .map(|metadata| metadata.map(|metadata| metadata.meta))
    }
    fn write_meta(&self, meta: &ObjectMeta, bytes: &[u8]) -> FaultResult<()> {
        let modified = meta.modified.duration_since(UNIX_EPOCH).map_err(|error| {
            Fault::new(
                Code::FailedPrecondition,
                "object metadata timestamp is before Unix epoch",
            )
            .with_source(error)
        })?;
        let modified_millis = u64::try_from(modified.as_millis()).map_err(|_| {
            Fault::new(
                Code::OutOfRange,
                "object metadata timestamp exceeds u64 milliseconds",
            )
        })?;
        let chunks = bytes
            .chunks(usize::try_from(VERIFICATION_CHUNK_BYTES).map_err(|_| {
                Fault::new(
                    Code::OutOfRange,
                    "object chunk size exceeds platform limits",
                )
            })?)
            .map(hash_bytes)
            .collect::<Vec<_>>();
        let mut text = format!(
            "{METADATA_V2_HEADER}\n{}\n{}\n{}\n{}\n{}\n{}\n",
            meta.version.generation(),
            meta.digest,
            modified_millis,
            meta.size.get(),
            VERIFICATION_CHUNK_BYTES,
            chunks.len(),
        );
        for digest in chunks {
            text.push_str(&digest.to_string());
            text.push('\n');
        }
        let digest = hash_bytes(meta.path.as_str().as_bytes()).to_hex();
        let path = RelativePath::new(format!("{digest}.meta"))
            .map_err(|error| Fault::internal(error.to_string()))?;
        self.metadata_files.publish(path, text.as_bytes(), true)?;
        Ok(())
    }
    fn collect_paths(
        root: &Path,
        current: &Path,
        output: &mut Vec<ObjectPath>,
        limit: usize,
    ) -> FaultResult<()> {
        if output.len() >= limit {
            return Ok(());
        }
        for entry in fs::read_dir(current).map_err(|error| {
            Fault::internal("failed to list object directory").with_source(error)
        })? {
            let entry = entry.map_err(|error| {
                Fault::internal("failed to read object directory entry").with_source(error)
            })?;
            let path = entry.path();
            if path
                .file_name()
                .is_some_and(|name| name == ".mindclade-object-metadata")
            {
                continue;
            }
            let file_type = entry.file_type().map_err(|error| {
                Fault::internal("failed to inspect object entry").with_source(error)
            })?;
            if file_type.is_dir() {
                Self::collect_paths(root, &path, output, limit)?;
            } else if file_type.is_file() {
                let relative = path
                    .strip_prefix(root)
                    .map_err(|error| Fault::internal("object escaped root").with_source(error))?;
                let encoded = relative.to_string_lossy().replace('\\', "/");
                output.push(
                    ObjectPath::new(encoded)
                        .map_err(|error| Fault::data_loss(error.to_string()))?,
                );
            }
            if output.len() >= limit {
                break;
            }
        }
        Ok(())
    }
}

fn object_file_metadata(path: &Path) -> FaultResult<Option<fs::Metadata>> {
    match fs::metadata(path) {
        Ok(metadata) if metadata.is_file() => Ok(Some(metadata)),
        Ok(_) => Err(Fault::data_loss("object path is not a regular file")),
        Err(error) if error.kind() == std::io::ErrorKind::NotFound => Ok(None),
        Err(error) => Err(Fault::internal("failed to stat object").with_source(error)),
    }
}

fn read_bounded_metadata(path: &Path) -> FaultResult<Vec<u8>> {
    let metadata_size = fs::metadata(path)
        .map_err(|error| Fault::data_loss("object metadata is missing").with_source(error))?
        .len();
    if metadata_size == 0 || metadata_size > MAXIMUM_METADATA_BYTES {
        return Err(Fault::data_loss("object metadata size is invalid"));
    }
    let capacity = usize::try_from(metadata_size).map_err(|_| {
        Fault::new(
            Code::ResourceExhausted,
            "object metadata exceeds platform limits",
        )
    })?;
    let mut bytes = Vec::with_capacity(capacity);
    fs::File::open(path)
        .map_err(|error| Fault::data_loss("object metadata is missing").with_source(error))?
        .take(MAXIMUM_METADATA_BYTES + 1)
        .read_to_end(&mut bytes)
        .map_err(|error| Fault::data_loss("object metadata read failed").with_source(error))?;
    if u64::try_from(bytes.len()).map_or(true, |size| size > MAXIMUM_METADATA_BYTES) {
        return Err(Fault::data_loss("object metadata exceeds its size limit"));
    }
    Ok(bytes)
}

fn parse_metadata_u64<'a>(
    lines: &mut impl Iterator<Item = &'a str>,
    field: &'static str,
) -> FaultResult<u64> {
    lines
        .next()
        .ok_or_else(|| Fault::data_loss(format!("{field} is missing")))?
        .parse::<u64>()
        .map_err(|error| Fault::data_loss(format!("{field} is invalid")).with_source(error))
}

fn chunk_count(size: u64) -> FaultResult<u64> {
    if size == 0 {
        return Ok(0);
    }
    size.checked_add(VERIFICATION_CHUNK_BYTES - 1)
        .map(|rounded| rounded / VERIFICATION_CHUNK_BYTES)
        .ok_or_else(|| Fault::new(Code::OutOfRange, "object chunk count overflow"))
}

fn read_chunk_verified_range(
    file: &mut fs::File,
    metadata: &LocalMetadata,
    chunks: &[Digest],
    range: ByteRange,
) -> FaultResult<Vec<u8>> {
    if range.is_empty() {
        return Ok(Vec::new());
    }
    let first_chunk = range.start() / VERIFICATION_CHUNK_BYTES;
    let last_chunk = (range.end() - 1) / VERIFICATION_CHUNK_BYTES;
    let first_offset = first_chunk
        .checked_mul(VERIFICATION_CHUNK_BYTES)
        .ok_or_else(|| Fault::new(Code::OutOfRange, "object chunk offset overflow"))?;
    file.seek(SeekFrom::Start(first_offset)).map_err(|error| {
        Fault::new(Code::Unavailable, "failed to seek verified object range").with_source(error)
    })?;
    let selected_length = usize::try_from(range.length())
        .map_err(|_| Fault::new(Code::OutOfRange, "object range exceeds platform limits"))?;
    let mut selected = Vec::with_capacity(selected_length);
    for chunk_index in first_chunk..=last_chunk {
        let chunk_start = chunk_index
            .checked_mul(VERIFICATION_CHUNK_BYTES)
            .ok_or_else(|| Fault::new(Code::OutOfRange, "object chunk offset overflow"))?;
        let remaining = metadata
            .meta
            .size
            .get()
            .checked_sub(chunk_start)
            .ok_or_else(|| Fault::data_loss("object chunk starts beyond object size"))?;
        let chunk_length = remaining.min(VERIFICATION_CHUNK_BYTES);
        let chunk_length = usize::try_from(chunk_length)
            .map_err(|_| Fault::new(Code::OutOfRange, "object chunk exceeds platform limits"))?;
        let mut bytes = vec![0_u8; chunk_length];
        file.read_exact(&mut bytes).map_err(|error| {
            Fault::data_loss("object is truncated relative to its chunk manifest")
                .with_source(error)
        })?;
        let expected = chunks
            .get(usize::try_from(chunk_index).map_err(|_| {
                Fault::new(
                    Code::OutOfRange,
                    "object chunk index exceeds platform limits",
                )
            })?)
            .ok_or_else(|| Fault::data_loss("object chunk digest is missing"))?;
        if hash_bytes(&bytes) != *expected {
            return Err(Fault::data_loss("object chunk digest mismatch"));
        }
        let chunk_end = chunk_start
            .checked_add(
                u64::try_from(chunk_length)
                    .map_err(|_| Fault::new(Code::OutOfRange, "object chunk length exceeds u64"))?,
            )
            .ok_or_else(|| Fault::new(Code::OutOfRange, "object chunk end overflow"))?;
        let selected_start = range.start().max(chunk_start);
        let selected_end = range.end().min(chunk_end);
        if selected_start < selected_end {
            let start = usize::try_from(selected_start - chunk_start).map_err(|_| {
                Fault::new(
                    Code::OutOfRange,
                    "object range offset exceeds platform limits",
                )
            })?;
            let end = usize::try_from(selected_end - chunk_start).map_err(|_| {
                Fault::new(
                    Code::OutOfRange,
                    "object range offset exceeds platform limits",
                )
            })?;
            selected.extend_from_slice(&bytes[start..end]);
        }
    }
    if selected.len() != selected_length {
        return Err(Fault::data_loss(
            "verified object range length does not match request",
        ));
    }
    Ok(selected)
}

fn read_legacy_verified_range(
    file: &mut fs::File,
    meta: &ObjectMeta,
    range: ByteRange,
) -> FaultResult<Vec<u8>> {
    let length = usize::try_from(range.length())
        .map_err(|_| Fault::new(Code::OutOfRange, "object range exceeds platform limits"))?;
    let mut selected = Vec::with_capacity(length);
    let mut digest = Sha256::new();
    let mut offset = 0_u64;
    let mut buffer = vec![0_u8; 64 * 1024];
    loop {
        let read = file.read(&mut buffer).map_err(|error| {
            Fault::internal("failed to read object range source").with_source(error)
        })?;
        if read == 0 {
            break;
        }
        let read_u64 = u64::try_from(read)
            .map_err(|_| Fault::new(Code::OutOfRange, "object read exceeds u64"))?;
        let next = offset
            .checked_add(read_u64)
            .ok_or_else(|| Fault::new(Code::OutOfRange, "object length overflow"))?;
        if next > meta.size.get() {
            return Err(Fault::data_loss("object is longer than its metadata"));
        }
        digest.update(&buffer[..read]);
        let selected_start = range.start().max(offset);
        let selected_end = range.end().min(next);
        if selected_start < selected_end {
            let start = usize::try_from(selected_start - offset).map_err(|_| {
                Fault::new(Code::OutOfRange, "range offset exceeds platform limits")
            })?;
            let end = usize::try_from(selected_end - offset).map_err(|_| {
                Fault::new(Code::OutOfRange, "range offset exceeds platform limits")
            })?;
            selected.extend_from_slice(&buffer[start..end]);
        }
        offset = next;
    }
    if offset != meta.size.get() || selected.len() != length {
        return Err(Fault::data_loss(
            "object is truncated relative to its metadata",
        ));
    }
    if digest.finalize() != meta.digest {
        return Err(Fault::data_loss("object digest mismatch"));
    }
    Ok(selected)
}

impl ObjectStore for LocalStore {
    fn head(&self, path: &ObjectPath) -> FaultResult<Option<ObjectMeta>> {
        Self::relative(path)?;
        let _guard = self
            .operations
            .read()
            .unwrap_or_else(std::sync::PoisonError::into_inner);
        self.read_meta(path)
    }
    fn get(&self, path: &ObjectPath, maximum_bytes: ByteSize) -> FaultResult<Vec<u8>> {
        let relative = Self::relative(path)?;
        let _guard = self
            .operations
            .read()
            .unwrap_or_else(std::sync::PoisonError::into_inner);
        let meta = self
            .read_meta(path)?
            .ok_or_else(|| Fault::new(Code::NotFound, "object not found"))?;
        self.files
            .read_verified(&relative, meta.digest, maximum_bytes.get())
    }
    fn get_range(&self, path: &ObjectPath, range: ByteRange) -> FaultResult<Vec<u8>> {
        Self::relative(path)?;
        let _guard = self
            .operations
            .read()
            .unwrap_or_else(std::sync::PoisonError::into_inner);
        let metadata = self
            .read_metadata(path)?
            .ok_or_else(|| Fault::new(Code::NotFound, "object not found"))?;
        if range.end() > metadata.meta.size.get() {
            return Err(Fault::new(
                Code::OutOfRange,
                "object range exceeds object size",
            ));
        }
        let mut file = fs::File::open(self.absolute(path))
            .map_err(|error| Fault::internal("failed to open object").with_source(error))?;
        if let Some(chunks) = metadata.chunks.as_deref() {
            read_chunk_verified_range(&mut file, &metadata, chunks, range)
        } else {
            read_legacy_verified_range(&mut file, &metadata.meta, range)
        }
    }
    fn put(
        &self,
        path: &ObjectPath,
        bytes: &[u8],
        condition: PutCondition,
    ) -> FaultResult<PutResult> {
        let relative = Self::relative(path)?;
        let _guard = self
            .operations
            .write()
            .unwrap_or_else(std::sync::PoisonError::into_inner);
        let current = self.read_meta(path)?;
        match (condition, current.as_ref()) {
            (PutCondition::Any, _) | (PutCondition::CreateOnly, None) => {}
            (PutCondition::CreateOnly, Some(_)) => {
                return Err(Fault::new(Code::AlreadyExists, "object already exists"));
            }
            (PutCondition::Match(expected), Some(actual)) if expected == actual.version => {}
            (PutCondition::Match(_), _) => {
                return Err(Fault::new(
                    Code::Conflict,
                    "object version precondition failed",
                ));
            }
        }
        let overwrite = !matches!(condition, PutCondition::CreateOnly);
        let published = self.files.publish(relative, bytes, overwrite)?;
        let digest = published.digest;
        let version = match current.as_ref() {
            Some(value) => value.version.next(digest)?,
            None => ResourceVersion::new(1, digest),
        };
        let meta = ObjectMeta {
            path: path.clone(),
            size: ByteSize::new(published.size),
            digest,
            version,
            modified: SystemTime::now(),
        };
        self.write_meta(&meta, bytes)?;
        Ok(PutResult {
            meta,
            created: current.is_none(),
        })
    }
    fn delete(&self, path: &ObjectPath, expected: Option<ResourceVersion>) -> FaultResult<bool> {
        Self::relative(path)?;
        let _guard = self
            .operations
            .write()
            .unwrap_or_else(std::sync::PoisonError::into_inner);
        let Some(meta) = self.read_meta(path)? else {
            return Ok(false);
        };
        if expected.is_some_and(|value| value != meta.version) {
            return Err(Fault::new(
                Code::Conflict,
                "object version precondition failed",
            ));
        }
        fs::remove_file(self.absolute(path))
            .map_err(|error| Fault::internal("failed to delete object").with_source(error))?;
        match fs::remove_file(self.metadata_path(path)) {
            Ok(()) => {}
            Err(error) if error.kind() == std::io::ErrorKind::NotFound => {}
            Err(error) => {
                return Err(Fault::internal("failed to delete object metadata").with_source(error));
            }
        }
        Ok(true)
    }
    fn list(&self, prefix: Option<&ObjectPath>, limit: usize) -> FaultResult<Vec<ObjectMeta>> {
        if limit == 0 || limit > 100_000 {
            return Err(Fault::invalid_argument("object list limit is invalid"));
        }
        if let Some(prefix) = prefix {
            Self::relative(prefix)?;
        }
        let _guard = self
            .operations
            .read()
            .unwrap_or_else(std::sync::PoisonError::into_inner);
        let mut paths = Vec::new();
        Self::collect_paths(
            self.files.root(),
            self.files.root(),
            &mut paths,
            MAXIMUM_LIST_CANDIDATES + 1,
        )?;
        if paths.len() > MAXIMUM_LIST_CANDIDATES {
            return Err(Fault::new(
                Code::ResourceExhausted,
                "local object listing exceeds bounded candidate scan",
            ));
        }
        paths.sort();
        let prefix = prefix.map(ObjectPath::as_str);
        let mut output = Vec::new();
        for path in paths {
            if prefix.is_some_and(|value| !path.as_str().starts_with(value)) {
                continue;
            }
            if let Some(meta) = self.read_meta(&path)? {
                output.push(meta);
            }
            if output.len() == limit {
                break;
            }
        }
        Ok(output)
    }
}
