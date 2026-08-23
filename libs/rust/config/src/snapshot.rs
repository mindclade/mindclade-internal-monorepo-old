// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

//! An immutable resolved configuration with provenance, redaction, and a digest.
//!
//! Resolution happens in two stages on purpose.
//!
//! **Load time** settles presence, the raw-value policy, unknown keys,
//! provenance, and the digest. Everything a snapshot can decide without knowing
//! what a value will be used *for* is decided once, at the composition root.
//!
//! **Read time** settles the type and the range, through the accessors below.
//! That split is not stylistic: `services/runtime_host` reads
//! `MINDCLADE_RUNTIME_MODEL_GPU_MEMORY_BYTES` as a positive integer only when
//! the preloaded-model group is configured, and validating it at load time
//! would refuse to start every host that runs without a preloaded model.

use crate::error;
use crate::field::Field;
use crate::secret::{REDACTED, Secret};
use mindclade_content_digest::{Digest, Sha256, hash_bytes};
use mindclade_faults::{Fault, FaultResult};
use std::collections::BTreeMap;
use std::fmt;
use std::path::{Component, PathBuf};
use std::str::FromStr;
use std::time::Duration;

/// Where a resolved value came from.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum Provenance {
    /// The field's declared default; no source supplied a value.
    Default,
    /// A configuration source supplied the value.
    Source,
}

/// Provenance and policy metadata for one resolved value.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct Origin {
    source: String,
    provenance: Provenance,
    secret: bool,
    reloadable: bool,
}

impl Origin {
    /// The provenance label: a source name, or `default`.
    #[must_use]
    pub fn source(&self) -> &str {
        &self.source
    }

    /// Whether the value came from a source or from the declared default.
    #[must_use]
    pub fn provenance(&self) -> Provenance {
        self.provenance
    }

    /// Whether the value is a credential.
    #[must_use]
    pub fn is_secret(&self) -> bool {
        self.secret
    }

    /// Whether the value may change without a restart.
    #[must_use]
    pub fn is_reloadable(&self) -> bool {
        self.reloadable
    }

    pub(crate) fn new(source: impl Into<String>, provenance: Provenance, field: &Field) -> Self {
        Self {
            source: source.into(),
            provenance,
            secret: field.is_secret(),
            reloadable: field.is_reloadable(),
        }
    }
}

/// An immutable, digest-carrying resolved configuration.
#[derive(Clone)]
pub struct Snapshot {
    namespace: String,
    values: BTreeMap<String, String>,
    origins: BTreeMap<String, Origin>,
    digest: Digest,
}

/// `Debug` renders the redacted view. A snapshot holds credentials, and a
/// `{:?}` on a config struct is exactly how they reach logs.
impl fmt::Debug for Snapshot {
    fn fmt(&self, formatter: &mut fmt::Formatter<'_>) -> fmt::Result {
        formatter
            .debug_struct("Snapshot")
            .field("namespace", &self.namespace)
            .field("digest", &self.digest)
            .field("values", &self.redacted())
            .finish_non_exhaustive()
    }
}

impl Snapshot {
    pub(crate) fn new(
        namespace: String,
        values: BTreeMap<String, String>,
        origins: BTreeMap<String, Origin>,
    ) -> FaultResult<Self> {
        let digest = compute_digest(&namespace, &values, &origins)?;
        Ok(Self {
            namespace,
            values,
            origins,
            digest,
        })
    }

    /// The owning namespace label.
    #[must_use]
    pub fn namespace(&self) -> &str {
        &self.namespace
    }

    /// A deterministic digest over every key, value, and provenance label.
    ///
    /// Secret values are hashed before they enter the digest input, so neither
    /// the digest nor the bytes it is computed over carry plaintext.
    #[must_use]
    pub fn digest(&self) -> Digest {
        self.digest
    }

    /// Every resolved key, in deterministic order.
    pub fn keys(&self) -> impl Iterator<Item = &str> {
        self.values.keys().map(String::as_str)
    }

    /// The provenance record for a key.
    #[must_use]
    pub fn origin(&self, key: &str) -> Option<&Origin> {
        self.origins.get(key)
    }

    /// A log-safe view of every value, with secrets replaced by `[REDACTED]`.
    #[must_use]
    pub fn redacted(&self) -> BTreeMap<String, String> {
        self.values
            .iter()
            .map(|(key, value)| {
                let secret = self.origins.get(key).is_some_and(Origin::is_secret);
                let rendered = if secret {
                    REDACTED.to_owned()
                } else {
                    value.clone()
                };
                (key.clone(), rendered)
            })
            .collect()
    }

    /// Whether two snapshots resolved to identical configuration.
    #[must_use]
    pub fn equivalent(&self, other: &Self) -> bool {
        self.digest.constant_time_eq(other.digest)
    }

    pub(crate) fn values(&self) -> &BTreeMap<String, String> {
        &self.values
    }

    pub(crate) fn origins(&self) -> &BTreeMap<String, Origin> {
        &self.origins
    }

    fn lookup(&self, key: &str) -> FaultResult<&str> {
        self.values.get(key).map(String::as_str).ok_or_else(|| {
            // A read of an undeclared key is a defect in the composition root,
            // not operator error, so it is reported the same way an undeclared
            // key from a source is.
            error::unknown_key(&self.namespace, key, "snapshot")
        })
    }

    fn non_secret(&self, key: &str) -> FaultResult<&str> {
        if self.origins.get(key).is_some_and(Origin::is_secret) {
            return Err(error::invalid(
                &self.namespace,
                key,
                "secret values must be read through Snapshot::secret",
            ));
        }
        self.lookup(key)
    }

    /// Whether a source supplied the value, as opposed to the declared default.
    pub fn is_set(&self, key: &str) -> FaultResult<bool> {
        self.lookup(key)?;
        Ok(self
            .origins
            .get(key)
            .is_some_and(|origin| origin.provenance() == Provenance::Source))
    }

    /// The raw value of a non-secret key.
    pub fn raw(&self, key: &str) -> FaultResult<&str> {
        self.non_secret(key)
    }

    /// The value of a non-secret key as an owned `String`.
    pub fn string(&self, key: &str) -> FaultResult<String> {
        self.non_secret(key).map(str::to_owned)
    }

    /// The value of a key as a [`Secret`].
    pub fn secret(&self, key: &str) -> FaultResult<Secret> {
        self.lookup(key).map(Secret::new)
    }

    /// Whether a non-secret value equals an exact literal.
    ///
    /// This is the honest spelling of the "flag" reads these services already
    /// had: `MINDCLADE_AI_GATEWAY_ALLOW_INSECURE_CONTROL_HTTP` enables insecure
    /// control transport only on the exact string `true`, and anything else —
    /// `TRUE`, `1`, empty, unset — leaves it off. A permissive boolean parser
    /// here would turn a typo into a downgrade of transport security.
    pub fn equals(&self, key: &str, expected: &str) -> FaultResult<bool> {
        Ok(self.non_secret(key)? == expected)
    }

    /// The value parsed through [`FromStr`], with the parse error attached.
    pub fn parse<T>(&self, key: &str) -> FaultResult<T>
    where
        T: FromStr,
        T::Err: std::error::Error + Send + Sync + 'static,
    {
        let value = self.non_secret(key)?;
        value.parse::<T>().map_err(|parse_error| {
            error::invalid(&self.namespace, key, "value does not parse").with_source(parse_error)
        })
    }

    /// The value as a `u64`. Zero is accepted.
    pub fn u64(&self, key: &str) -> FaultResult<u64> {
        self.parse::<u64>(key)
    }

    /// The value as a `u64` that must be greater than zero.
    pub fn u64_positive(&self, key: &str) -> FaultResult<u64> {
        let value = self.u64(key)?;
        if value == 0 {
            return Err(error::invalid(
                &self.namespace,
                key,
                "value must be positive",
            ));
        }
        Ok(value)
    }

    /// The value as a positive `u32`; a value too wide reports out-of-range.
    pub fn u32_positive(&self, key: &str) -> FaultResult<u32> {
        let value = self.u64_positive(key)?;
        u32::try_from(value)
            .map_err(|_| error::out_of_range(&self.namespace, key, "value exceeds u32"))
    }

    /// The value as a positive `usize`; a value too wide reports out-of-range.
    pub fn usize_positive(&self, key: &str) -> FaultResult<usize> {
        let value = self.u64_positive(key)?;
        usize::try_from(value)
            .map_err(|_| error::out_of_range(&self.namespace, key, "value exceeds usize"))
    }

    /// The value as a `u64` inside an inclusive range.
    pub fn u64_bounded(&self, key: &str, minimum: u64, maximum: u64) -> FaultResult<u64> {
        let value = self.u64(key)?;
        if value < minimum || value > maximum {
            return Err(self.out_of_bounds(key));
        }
        Ok(value)
    }

    /// The value as a `u32` inside an inclusive range.
    pub fn u32_bounded(&self, key: &str, minimum: u32, maximum: u32) -> FaultResult<u32> {
        let value = self.u64_bounded(key, u64::from(minimum), u64::from(maximum))?;
        u32::try_from(value)
            .map_err(|_| error::out_of_range(&self.namespace, key, "value exceeds u32"))
    }

    /// The value as a `usize` inside an inclusive range.
    pub fn usize_bounded(&self, key: &str, minimum: usize, maximum: usize) -> FaultResult<usize> {
        let value = self.parse::<usize>(key)?;
        if value < minimum || value > maximum {
            return Err(self.out_of_bounds(key));
        }
        Ok(value)
    }

    /// The value as a whole number of seconds inside an inclusive range.
    pub fn duration_seconds_bounded(
        &self,
        key: &str,
        minimum: u64,
        maximum: u64,
    ) -> FaultResult<Duration> {
        self.u64_bounded(key, minimum, maximum)
            .map(Duration::from_secs)
    }

    /// The value as a filesystem path.
    pub fn path(&self, key: &str) -> FaultResult<PathBuf> {
        Ok(PathBuf::from(self.non_secret(key)?))
    }

    /// The value as an absolute filesystem path.
    pub fn absolute_path(&self, key: &str) -> FaultResult<PathBuf> {
        let path = self.path(key)?;
        if !path.is_absolute() {
            return Err(error::invalid(
                &self.namespace,
                key,
                "path must be absolute",
            ));
        }
        Ok(path)
    }

    /// The value as an absolute path with no `.`/`..` component.
    ///
    /// Used for paths that name material a process will read at privilege, such
    /// as a CA bundle, where a traversal component is the whole attack.
    pub fn resolved_absolute_path(&self, key: &str, maximum_bytes: usize) -> FaultResult<PathBuf> {
        let value = self.non_secret(key)?;
        if value.len() > maximum_bytes {
            return Err(error::invalid(
                &self.namespace,
                key,
                "path exceeds its byte ceiling",
            ));
        }
        let path = PathBuf::from(value);
        let traversal = path
            .components()
            .any(|component| matches!(component, Component::ParentDir | Component::CurDir));
        if !path.is_absolute() || traversal {
            return Err(error::invalid(
                &self.namespace,
                key,
                "path must be absolute and free of traversal components",
            ));
        }
        Ok(path)
    }

    fn out_of_bounds(&self, key: &str) -> Fault {
        error::invalid(&self.namespace, key, "value is outside its declared bounds")
    }
}

/// Length-prefixed, order-independent encoding of the resolved configuration.
///
/// Length prefixes rather than separators: with `key=value` joined by a
/// delimiter, a value containing the delimiter can forge a different
/// configuration that hashes the same.
fn compute_digest(
    namespace: &str,
    values: &BTreeMap<String, String>,
    origins: &BTreeMap<String, Origin>,
) -> FaultResult<Digest> {
    let mut hasher = Sha256::new();
    write_framed(&mut hasher, namespace.as_bytes(), namespace)?;
    for (key, value) in values {
        let origin = origins.get(key);
        let secret = origin.is_some_and(Origin::is_secret);
        let source = origin.map_or("", Origin::source);
        write_framed(&mut hasher, key.as_bytes(), namespace)?;
        hasher.update(&[u8::from(secret)]);
        if secret {
            // The digest input itself never contains plaintext: a secret enters
            // as its own SHA-256, so the digest still changes on rotation while
            // the bytes being hashed stay safe to hand to a debugger.
            let opaque = hash_bytes(value.as_bytes());
            write_framed(&mut hasher, opaque.as_bytes(), namespace)?;
        } else {
            write_framed(&mut hasher, value.as_bytes(), namespace)?;
        }
        write_framed(&mut hasher, source.as_bytes(), namespace)?;
    }
    Ok(hasher.finalize())
}

fn write_framed(hasher: &mut Sha256, bytes: &[u8], namespace: &str) -> FaultResult<()> {
    let length = u64::try_from(bytes.len())
        .map_err(|_| error::internal(namespace, "digest frame length exceeds u64"))?;
    hasher.update(&length.to_be_bytes());
    hasher.update(bytes);
    Ok(())
}
