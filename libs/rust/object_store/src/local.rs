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
use std::io::Read;
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
    fn read_meta(&self, path: &ObjectPath) -> FaultResult<Option<ObjectMeta>> {
        let absolute = self.absolute(path);
        let file_metadata = match fs::metadata(&absolute) {
            Ok(value) => value,
            Err(error) if error.kind() == std::io::ErrorKind::NotFound => return Ok(None),
            Err(error) => return Err(Fault::internal("failed to stat object").with_source(error)),
        };
        if !file_metadata.is_file() {
            return Err(Fault::data_loss("object path is not a regular file"));
        }
        let bytes = fs::read(self.metadata_path(path))
            .map_err(|error| Fault::data_loss("object metadata is missing").with_source(error))?;
        let text = std::str::from_utf8(&bytes)
            .map_err(|error| Fault::data_loss("object metadata is not UTF-8").with_source(error))?;
        let mut lines = text.lines();
        let generation = lines
            .next()
            .ok_or_else(|| Fault::data_loss("object metadata generation is missing"))?
            .parse::<u64>()
            .map_err(|error| {
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
        let size = file_metadata.len();
        Ok(Some(ObjectMeta {
            path: path.clone(),
            size: ByteSize::new(size),
            digest,
            version: ResourceVersion::new(generation, digest),
            modified: UNIX_EPOCH + Duration::from_millis(modified_millis),
        }))
    }
    fn write_meta(&self, meta: &ObjectMeta) -> FaultResult<()> {
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
        let text = format!(
            "{}\n{}\n{}\n",
            meta.version.generation(),
            meta.digest,
            modified_millis
        );
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
        let meta = self
            .read_meta(path)?
            .ok_or_else(|| Fault::new(Code::NotFound, "object not found"))?;
        if range.end() > meta.size.get() {
            return Err(Fault::new(
                Code::OutOfRange,
                "object range exceeds object size",
            ));
        }
        let mut file = fs::File::open(self.absolute(path))
            .map_err(|error| Fault::internal("failed to open object").with_source(error))?;
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
        self.write_meta(&meta)?;
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
