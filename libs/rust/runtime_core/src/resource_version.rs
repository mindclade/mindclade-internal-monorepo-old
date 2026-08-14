//! Content-bound optimistic concurrency versions.
#![forbid(unsafe_code)]

use core::fmt;
use core::str::FromStr;
use mindclade_content_digest::Digest;
use mindclade_faults::{Code, Fault, FaultResult};

/// Version combining a monotonic generation and content digest.
#[derive(Clone, Copy, Debug, Eq, Hash, Ord, PartialEq, PartialOrd)]
pub struct ResourceVersion {
    generation: u64,
    digest: Digest,
}

impl ResourceVersion {
    #[must_use]
    pub const fn new(generation: u64, digest: Digest) -> Self {
        Self { generation, digest }
    }
    #[must_use]
    pub const fn generation(self) -> u64 {
        self.generation
    }
    #[must_use]
    pub const fn digest(self) -> Digest {
        self.digest
    }
    pub fn next(self, digest: Digest) -> FaultResult<Self> {
        let generation = self
            .generation
            .checked_add(1)
            .ok_or_else(|| Fault::new(Code::OutOfRange, "resource-version generation overflow"))?;
        Ok(Self::new(generation, digest))
    }
}

impl fmt::Display for ResourceVersion {
    fn fmt(&self, formatter: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(formatter, "rv1:{}:{}", self.generation, self.digest)
    }
}

impl FromStr for ResourceVersion {
    type Err = Fault;
    fn from_str(value: &str) -> FaultResult<Self> {
        let mut parts = value.splitn(3, ':');
        if parts.next() != Some("rv1") {
            return Err(Fault::invalid_argument("resource-version schema is invalid"));
        }
        let generation = parts
            .next()
            .ok_or_else(|| Fault::invalid_argument("resource-version generation is missing"))?
            .parse::<u64>()
            .map_err(|error| Fault::invalid_argument("resource-version generation is invalid").with_source(error))?;
        let digest = parts
            .next()
            .ok_or_else(|| Fault::invalid_argument("resource-version digest is missing"))?
            .parse::<Digest>()
            .map_err(|error| Fault::invalid_argument("resource-version digest is invalid").with_source(error))?;
        Ok(Self::new(generation, digest))
    }
}

/// Conditional write contract.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum Precondition {
    Any,
    MustExist,
    MustNotExist,
    Match(ResourceVersion),
}

impl Precondition {
    pub fn check(self, current: Option<ResourceVersion>) -> FaultResult<()> {
        match (self, current) {
            (Self::Any, _) | (Self::MustExist, Some(_)) | (Self::MustNotExist, None) => Ok(()),
            (Self::Match(expected), Some(actual)) if expected == actual => Ok(()),
            (Self::MustExist, None) => Err(Fault::new(Code::NotFound, "resource does not exist")),
            (Self::MustNotExist, Some(_)) => {
                Err(Fault::new(Code::AlreadyExists, "resource already exists"))
            }
            (Self::Match(_), _) => Err(Fault::new(
                Code::Conflict,
                "resource-version precondition failed",
            )),
        }
    }
}
