//! Atomic file publication.

use crate::RelativePath;
use mindclade_runtime_core::{Clock, SystemClock};
use mindclade_content_digest::{hash_reader, Digest};
use mindclade_faults::{Code, Fault, FaultResult};
use mindclade_identifiers::ResourceId;
use std::fs::{self, File, OpenOptions};
use std::io::{Read, Write};
use std::path::{Path, PathBuf};

/// Metadata returned after an atomic publication.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct PublishedFile {
    pub path: RelativePath,
    pub size: u64,
    pub digest: Digest,
}

/// Atomic filesystem rooted at one trusted directory.
#[derive(Clone, Debug)]
pub struct AtomicFileStore {
    root: PathBuf,
}

impl AtomicFileStore {
    pub fn new(root: impl Into<PathBuf>) -> FaultResult<Self> {
        let root = root.into();
        fs::create_dir_all(&root)
            .map_err(|error| Fault::internal("failed to create atomic-file root").with_source(error))?;
        Ok(Self { root })
    }
    #[must_use]
    pub fn root(&self) -> &Path {
        &self.root
    }
    pub fn publish(&self, path: RelativePath, bytes: &[u8], overwrite: bool) -> FaultResult<PublishedFile> {
        let destination = self.root.join(path.as_path());
        let Some(parent) = destination.parent() else {
            return Err(Fault::invalid_argument("destination has no parent"));
        };
        fs::create_dir_all(parent)
            .map_err(|error| Fault::internal("failed to create destination directory").with_source(error))?;
        if !overwrite && destination.exists() {
            return Err(Fault::new(Code::AlreadyExists, "destination already exists"));
        }
        let temporary = temporary_path(parent)?;
        let result = (|| {
            let mut file = OpenOptions::new()
                .write(true)
                .create_new(true)
                .open(&temporary)
                .map_err(|error| Fault::internal("failed to create temporary file").with_source(error))?;
            file.write_all(bytes)
                .map_err(|error| Fault::internal("failed to write temporary file").with_source(error))?;
            file.sync_all()
                .map_err(|error| Fault::internal("failed to synchronize temporary file").with_source(error))?;
            drop(file);
            if overwrite {
                match fs::rename(&temporary, &destination) {
                    Ok(()) => {}
                    Err(first_error) if destination.exists() => {
                        fs::remove_file(&destination).map_err(|error| {
                            Fault::internal("failed to replace destination").with_source(error)
                        })?;
                        fs::rename(&temporary, &destination).map_err(|error| {
                            Fault::internal("failed to publish replacement").with_source(error)
                        })?;
                        let _ = first_error;
                    }
                    Err(error) => {
                        return Err(Fault::internal("failed to publish destination atomically")
                            .with_source(error));
                    }
                }
            } else {
                fs::hard_link(&temporary, &destination).map_err(|error| {
                    if error.kind() == std::io::ErrorKind::AlreadyExists {
                        Fault::new(Code::AlreadyExists, "destination already exists")
                            .with_source(error)
                    } else {
                        Fault::internal("failed to publish create-only destination")
                            .with_source(error)
                    }
                })?;
                fs::remove_file(&temporary).map_err(|error| {
                    Fault::internal("failed to remove published temporary file").with_source(error)
                })?;
            }
            sync_directory(parent)?;
            Ok(PublishedFile {
                path,
                size: u64::try_from(bytes.len())
                    .map_err(|_| Fault::new(Code::OutOfRange, "published file size exceeds u64"))?,
                digest: mindclade_content_digest::hash_bytes(bytes),
            })
        })();
        if result.is_err() {
            let _ = fs::remove_file(&temporary);
        }
        result
    }
    pub fn read_verified(&self, path: &RelativePath, expected: Digest, maximum_bytes: u64) -> FaultResult<Vec<u8>> {
        let full_path = self.root.join(path.as_path());
        let metadata = fs::metadata(&full_path)
            .map_err(|error| Fault::new(Code::NotFound, "file not found").with_source(error))?;
        if metadata.len() > maximum_bytes {
            return Err(Fault::new(Code::ResourceExhausted, "file exceeds configured read limit"));
        }
        let capacity = usize::try_from(metadata.len())
            .map_err(|_| Fault::new(Code::ResourceExhausted, "file is too large for this process"))?;
        let mut file = File::open(&full_path)
            .map_err(|error| Fault::internal("failed to open file").with_source(error))?;
        let actual = hash_reader(&mut file)?;
        if !actual.constant_time_eq(expected) {
            return Err(Fault::data_loss("file digest mismatch"));
        }
        let mut bytes = Vec::with_capacity(capacity);
        let mut file = File::open(&full_path)
            .map_err(|error| Fault::internal("failed to reopen verified file").with_source(error))?;
        file.read_to_end(&mut bytes)
            .map_err(|error| Fault::internal("failed to read verified file").with_source(error))?;
        Ok(bytes)
    }
}

fn temporary_path(parent: &Path) -> FaultResult<PathBuf> {
    let clock = SystemClock;
    let identifier = ResourceId::generate("tmp", &clock)
        .map_err(|error| Fault::internal("failed to generate temporary filename").with_source(error))?;
    Ok(parent.join(format!(".{}.partial", identifier.body())))
}

fn sync_directory(path: &Path) -> FaultResult<()> {
    let directory = File::open(path)
        .map_err(|error| Fault::internal("failed to open directory for synchronization").with_source(error))?;
    directory
        .sync_all()
        .map_err(|error| Fault::internal("failed to synchronize directory").with_source(error))
}
