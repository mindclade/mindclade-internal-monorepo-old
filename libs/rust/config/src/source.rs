// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

//! Ordered configuration sources.
//!
//! Sources are merged in the order the caller lists them; a later source wins.
//! Loading is always an explicit call — there is no global, no `lazy_static`
//! that reads the environment on import, and no ambient state. `libs/rust`
//! foundation crates create no hidden process-wide effects (see
//! `libs/rust/SECURITY.md`), and configuration is the classic place that rule
//! gets broken.

use crate::error;
use crate::field::MAX_KEY_BYTES;
use mindclade_faults::FaultResult;
use std::collections::BTreeMap;
use std::fmt;

/// Largest number of entries a single source may produce.
pub const MAX_SOURCE_ENTRIES: usize = 512;
/// Largest permitted environment variable name, in bytes.
pub const MAX_VARIABLE_BYTES: usize = 256;

/// A named provider of raw configuration values.
pub trait Source: fmt::Debug {
    /// The provenance label recorded on every value this source supplies.
    fn name(&self) -> &str;

    /// Produces the raw values this source carries, keyed by canonical key.
    fn load(&self, namespace: &str) -> FaultResult<BTreeMap<String, String>>;

    /// The external name a key is bound to, when the source has one.
    ///
    /// Purely diagnostic: it lets a fault name `MINDCLADE_RUNTIME_KEY_ID`
    /// rather than only `runtime.key.id`, which is what an operator has to go
    /// and set. It is never used for resolution.
    fn external_name(&self, _key: &str) -> Option<&str> {
        None
    }
}

/// An in-memory source, used for file-backed layers, overrides, and tests.
#[derive(Clone, Debug)]
pub struct MapSource {
    name: String,
    values: BTreeMap<String, String>,
}

impl MapSource {
    /// Creates a named map source.
    #[must_use]
    pub fn new(name: impl Into<String>) -> Self {
        Self {
            name: name.into(),
            values: BTreeMap::new(),
        }
    }

    /// Adds one raw value.
    #[must_use]
    pub fn with(mut self, key: impl Into<String>, value: impl Into<String>) -> Self {
        self.values.insert(key.into(), value.into());
        self
    }
}

impl Source for MapSource {
    fn name(&self) -> &str {
        &self.name
    }

    fn load(&self, namespace: &str) -> FaultResult<BTreeMap<String, String>> {
        if self.values.len() > MAX_SOURCE_ENTRIES {
            return Err(error::source_failed(
                namespace,
                &self.name,
                "source exceeds its entry ceiling",
            ));
        }
        Ok(self.values.clone())
    }
}

/// Where an [`EnvSource`] reads variables from.
#[derive(Clone, Debug)]
enum Lookup {
    /// The real process environment.
    Process,
    /// An explicit table, so tests never mutate process-global state.
    Table(BTreeMap<String, String>),
}

/// Maps canonical configuration keys onto exact environment variable names.
///
/// It **never scans** the environment. Only the names in the mapping are read,
/// which is what makes accidental capture of an unrelated variable impossible
/// and keeps unknown-key rejection meaningful. This matches `EnvSource` in
/// `libs/go/config` deliberately.
#[derive(Clone, Debug)]
pub struct EnvSource {
    name: String,
    mapping: BTreeMap<String, String>,
    lookup: Lookup,
}

impl EnvSource {
    /// Creates a source that reads the real process environment.
    #[must_use]
    pub fn process() -> Self {
        Self {
            name: "environment".to_owned(),
            mapping: BTreeMap::new(),
            lookup: Lookup::Process,
        }
    }

    /// Creates a source that reads an explicit variable table.
    ///
    /// Tests use this instead of `std::env::set_var`, which edition 2024 gates
    /// behind an audited block and which races every other test in the same
    /// process.
    #[must_use]
    pub fn from_table(variables: BTreeMap<String, String>) -> Self {
        Self {
            name: "environment".to_owned(),
            mapping: BTreeMap::new(),
            lookup: Lookup::Table(variables),
        }
    }

    /// Overrides the provenance label.
    #[must_use]
    pub fn named(mut self, name: impl Into<String>) -> Self {
        self.name = name.into();
        self
    }

    /// Binds a canonical key to an environment variable name.
    #[must_use]
    pub fn bind(mut self, key: impl Into<String>, variable: impl Into<String>) -> Self {
        self.mapping.insert(key.into(), variable.into());
        self
    }

    /// Returns the variable a key is bound to, for diagnostics.
    #[must_use]
    pub fn variable(&self, key: &str) -> Option<&str> {
        self.mapping.get(key).map(String::as_str)
    }

    /// Returns every binding in deterministic key order.
    pub fn bindings(&self) -> impl Iterator<Item = (&str, &str)> {
        self.mapping
            .iter()
            .map(|(key, variable)| (key.as_str(), variable.as_str()))
    }

    fn read(&self, variable: &str) -> Result<Option<String>, &'static str> {
        match &self.lookup {
            Lookup::Table(table) => Ok(table.get(variable).cloned()),
            Lookup::Process => match std::env::var_os(variable) {
                None => Ok(None),
                // Fails closed. The loaders this replaces mapped a non-UTF-8
                // value onto "missing", which reports a malformed value as an
                // absent one and sends an operator looking for the wrong bug.
                Some(value) => value
                    .into_string()
                    .map(Some)
                    .map_err(|_| "environment value is not valid UTF-8"),
            },
        }
    }
}

impl Source for EnvSource {
    fn name(&self) -> &str {
        &self.name
    }

    fn external_name(&self, key: &str) -> Option<&str> {
        self.variable(key)
    }

    fn load(&self, namespace: &str) -> FaultResult<BTreeMap<String, String>> {
        if self.mapping.len() > MAX_SOURCE_ENTRIES {
            return Err(error::source_failed(
                namespace,
                &self.name,
                "source exceeds its entry ceiling",
            ));
        }
        let mut values = BTreeMap::new();
        for (key, variable) in &self.mapping {
            if key.len() > MAX_KEY_BYTES {
                return Err(error::source_failed(
                    namespace,
                    &self.name,
                    "bound key exceeds its byte ceiling",
                ));
            }
            if variable.is_empty() || variable.len() > MAX_VARIABLE_BYTES {
                return Err(error::invalid(
                    namespace,
                    key,
                    "environment variable name is empty or exceeds its byte ceiling",
                ));
            }
            match self.read(variable) {
                Ok(None) => {}
                Ok(Some(value)) => {
                    values.insert(key.clone(), value);
                }
                Err(detail) => {
                    return Err(error::invalid(namespace, key, detail)
                        .with_context(error::CONTEXT_VARIABLE, variable.clone()));
                }
            }
        }
        Ok(values)
    }
}
