// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

//! The declared field set for one process, and the load that resolves it.

use crate::error;
use crate::field::{EmptyValue, Field, Whitespace};
use crate::snapshot::{Origin, Provenance, Snapshot};
use crate::source::Source;
use mindclade_faults::{Fault, FaultResult};
use std::collections::BTreeMap;
use std::fmt::Write as _;

/// Largest number of fields one catalog may declare.
pub const MAX_FIELDS: usize = 512;
/// Largest number of sources one load may merge.
pub const MAX_SOURCES: usize = 8;
/// Largest permitted namespace label, in bytes.
pub const MAX_NAMESPACE_BYTES: usize = 64;
/// Provenance label recorded for values that came from a declared default.
pub const DEFAULT_SOURCE: &str = "default";

/// The complete configuration surface of one process.
///
/// A catalog is built explicitly at a composition root and handed to
/// [`Catalog::load`]. There is no global instance and nothing reads the
/// environment as a side effect of linking this crate.
#[derive(Clone, Debug)]
pub struct Catalog {
    namespace: String,
    fields: BTreeMap<String, Field>,
}

impl Catalog {
    /// Creates an empty catalog under a namespace label.
    ///
    /// The namespace appears in every fault message, which is how each migrated
    /// service keeps the diagnostic voice its operators already grep for
    /// (`runtime-host …`, `AI Gateway …`).
    pub fn new(namespace: impl Into<String>) -> FaultResult<Self> {
        let namespace = namespace.into();
        let printable = namespace
            .bytes()
            .all(|byte| byte.is_ascii_graphic() || byte == b' ');
        if namespace.trim().is_empty() || namespace.len() > MAX_NAMESPACE_BYTES || !printable {
            return Err(error::field_invalid(
                &namespace,
                "",
                "namespace must be a short printable label",
            ));
        }
        Ok(Self {
            namespace,
            fields: BTreeMap::new(),
        })
    }

    /// Declares one field, rejecting duplicates and malformed declarations.
    pub fn declare(mut self, field: Field) -> FaultResult<Self> {
        field.validate(&self.namespace)?;
        if self.fields.len() >= MAX_FIELDS {
            return Err(error::field_invalid(
                &self.namespace,
                field.key(),
                "catalog exceeds its field ceiling",
            ));
        }
        if self.fields.contains_key(field.key()) {
            return Err(error::field_invalid(
                &self.namespace,
                field.key(),
                "field is declared more than once",
            ));
        }
        self.fields.insert(field.key().to_owned(), field);
        Ok(self)
    }

    /// The namespace label.
    #[must_use]
    pub fn namespace(&self) -> &str {
        &self.namespace
    }

    /// Every declared field, in deterministic key order.
    pub fn fields(&self) -> impl Iterator<Item = &Field> {
        self.fields.values()
    }

    /// Looks up one declared field.
    #[must_use]
    pub fn field(&self, key: &str) -> Option<&Field> {
        self.fields.get(key)
    }

    /// Renders the whole configuration surface as a Markdown table.
    ///
    /// Mandatory field documentation exists so that this is always complete:
    /// the settings a service accepts are derivable from the catalog rather
    /// than from reading a bootstrap function, and a field cannot be added
    /// without describing it.
    #[must_use]
    pub fn documentation(&self) -> String {
        let mut rendered = format!("# {} configuration\n\n", self.namespace);
        rendered.push_str("| Key | Requirement | Secret | Reloadable | Description |\n");
        rendered.push_str("|---|---|---|---|---|\n");
        for field in self.fields.values() {
            let requirement = match field.default_value() {
                None => "required".to_owned(),
                Some("") => "optional (unset)".to_owned(),
                Some(default) => format!("default `{default}`"),
            };
            // Writing into a `String` cannot fail; there is no error to report.
            let _ = writeln!(
                rendered,
                "| `{}` | {} | {} | {} | {} |",
                field.key(),
                requirement,
                yes_no(field.is_secret()),
                yes_no(field.is_reloadable()),
                field.doc().replace('\n', " ").replace('|', "\\|"),
            );
        }
        rendered
    }

    /// Merges the sources in order and resolves every declared field.
    ///
    /// A key that no field declares is rejected wherever it comes from. Silently
    /// ignoring an unrecognized setting is fail-open: the operator believes the
    /// setting took effect and the process runs on the value they meant to
    /// replace. `libs/go/config` fails closed on this and so does this crate.
    pub fn load(&self, sources: &[&dyn Source]) -> FaultResult<Snapshot> {
        if self.fields.is_empty() {
            return Err(error::field_invalid(
                &self.namespace,
                "",
                "catalog declares no fields",
            ));
        }
        if sources.len() > MAX_SOURCES {
            return Err(error::source_failed(
                &self.namespace,
                "catalog",
                "load exceeds its source ceiling",
            ));
        }

        let mut supplied: BTreeMap<String, (String, String)> = BTreeMap::new();
        let mut variables: BTreeMap<&str, &str> = BTreeMap::new();
        for source in sources {
            for key in self.fields.keys() {
                if let Some(external) = source.external_name(key) {
                    variables.insert(key.as_str(), external);
                }
            }
            let name = source.name().to_owned();
            if name.trim().is_empty() || name == DEFAULT_SOURCE {
                return Err(error::source_failed(
                    &self.namespace,
                    &name,
                    "source name must be non-empty and must not shadow the default label",
                ));
            }
            for (key, value) in source.load(&self.namespace)? {
                let Some(field) = self.fields.get(&key) else {
                    return Err(error::unknown_key(&self.namespace, &key, &name));
                };
                if value.len() > field.value_limit() {
                    return Err(annotate(
                        error::invalid(&self.namespace, &key, "value exceeds its declared ceiling"),
                        variables.get(key.as_str()).copied(),
                    ));
                }
                supplied.insert(key, (value, name.clone()));
            }
        }

        let mut values = BTreeMap::new();
        let mut origins = BTreeMap::new();
        for (key, field) in &self.fields {
            let candidate = match supplied.get(key) {
                // An explicitly empty value is indistinguishable from an unset
                // one only where the field says so; elsewhere it stays empty and
                // fails at its own type, which is what the replaced loaders did.
                Some((value, _)) if value.is_empty() && field.empty() == EmptyValue::UseDefault => {
                    None
                }
                Some((value, source)) => Some((value.clone(), source.clone())),
                None => None,
            };
            let variable = variables.get(key.as_str()).copied();
            let (value, origin) = if let Some((value, source)) = candidate {
                Self::enforce_policy(&self.namespace, key, field, &value, variable)?;
                (value, Origin::new(source, Provenance::Source, field))
            } else {
                let Some(default) = field.default_value() else {
                    return Err(error::missing(&self.namespace, key, variable));
                };
                (
                    default.to_owned(),
                    Origin::new(DEFAULT_SOURCE, Provenance::Default, field),
                )
            };
            values.insert(key.clone(), value);
            origins.insert(key.clone(), origin);
        }
        Snapshot::new(self.namespace.clone(), values, origins)
    }

    fn enforce_policy(
        namespace: &str,
        key: &str,
        field: &Field,
        value: &str,
        variable: Option<&str>,
    ) -> FaultResult<()> {
        if field.treats_blank_as_missing() && value.trim().is_empty() {
            return Err(error::missing(namespace, key, variable));
        }
        let detail = if field.whitespace() == Whitespace::RejectSurrounding && value.trim() != value
        {
            Some("value must not carry leading or trailing whitespace")
        } else if field.is_required() && value.is_empty() {
            Some("value must not be empty")
        } else if value.len() > field.value_limit() {
            Some("value exceeds its declared ceiling")
        } else {
            None
        };
        match detail {
            None => Ok(()),
            Some(detail) => Err(annotate(error::invalid(namespace, key, detail), variable)),
        }
    }
}

fn annotate(fault: Fault, variable: Option<&str>) -> Fault {
    match variable {
        Some(name) => fault.with_context(error::CONTEXT_VARIABLE, name),
        None => fault,
    }
}

fn yes_no(value: bool) -> &'static str {
    if value { "yes" } else { "no" }
}
