// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

//! Declared configuration fields and the raw-value policy each one carries.
//!
//! A [`Field`] is the Rust counterpart of `libs/go/config`'s `Field`: a key
//! plus Required/Default/Secret/Reloadable metadata. It adds two things.
//!
//! First, **documentation is mandatory**. A field with an empty `doc` is
//! rejected when the catalog is built, so
//! [`Catalog::documentation`](crate::Catalog::documentation) can always render
//! the complete configuration surface. That is the idea worth borrowing from
//! Cloudflare's `foundations` `settings` module — the surface is visible by
//! construction rather than scattered across `Option` fields and `unwrap_or`
//! calls — without taking the dependency or a proc-macro.
//!
//! Second, the **raw-value policy is explicit** ([`Whitespace`],
//! [`EmptyValue`], `maximum_bytes`, `blank_is_missing`). The three hand-rolled
//! loaders this crate replaces did not agree on any of these: one rejected
//! surrounding whitespace and capped values at 4 KiB, one trimmed only to test
//! emptiness and returned the untrimmed value, and one treated an empty string
//! as "absent" for integers but as a parse error for URLs. Those differences
//! are real deployed behavior, so they are declared per field instead of being
//! averaged into one rule that would silently change how a live service starts.

use crate::error;
use mindclade_faults::FaultResult;

/// Largest permitted configuration key, in bytes.
pub const MAX_KEY_BYTES: usize = 128;
/// Largest permitted configuration value, in bytes.
///
/// Every value is bounded: an unbounded environment read is an unbounded
/// allocation, and the repository requires parsers to declare a ceiling.
pub const MAX_VALUE_BYTES: usize = 1 << 20;
/// Largest permitted field documentation string, in bytes.
pub const MAX_DOC_BYTES: usize = 4096;

/// How a value's surrounding whitespace is treated.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum Whitespace {
    /// The value is used exactly as supplied.
    Preserve,
    /// A value that differs from its trimmed form is rejected as invalid.
    RejectSurrounding,
}

/// How an explicitly empty value is treated.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum EmptyValue {
    /// An empty value is kept, and fails later only if its type rejects it.
    Verbatim,
    /// An empty value is indistinguishable from the setting being unset.
    UseDefault,
}

/// A declared configuration field.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct Field {
    key: String,
    doc: String,
    default: Option<String>,
    secret: bool,
    reloadable: bool,
    maximum_bytes: usize,
    whitespace: Whitespace,
    empty: EmptyValue,
    blank_is_missing: bool,
}

impl Field {
    /// Declares a field with no default: the process cannot start without it.
    #[must_use]
    pub fn required(key: impl Into<String>, doc: impl Into<String>) -> Self {
        Self::build(key, doc, None)
    }

    /// Declares a field with a working default.
    #[must_use]
    pub fn defaulted(
        key: impl Into<String>,
        doc: impl Into<String>,
        default: impl Into<String>,
    ) -> Self {
        Self::build(key, doc, Some(default.into()))
    }

    fn build(key: impl Into<String>, doc: impl Into<String>, default: Option<String>) -> Self {
        Self {
            key: key.into(),
            doc: doc.into(),
            default,
            secret: false,
            reloadable: false,
            maximum_bytes: MAX_VALUE_BYTES,
            whitespace: Whitespace::Preserve,
            empty: EmptyValue::Verbatim,
            blank_is_missing: false,
        }
    }

    /// Marks the value as a credential: redacted everywhere, hashed in digests.
    #[must_use]
    pub fn secret(mut self) -> Self {
        self.secret = true;
        self
    }

    /// Marks the value as safe to change without restarting the process.
    #[must_use]
    pub fn reloadable(mut self) -> Self {
        self.reloadable = true;
        self
    }

    /// Caps the value length. Values above the cap are rejected as invalid.
    #[must_use]
    pub fn maximum_bytes(mut self, limit: usize) -> Self {
        self.maximum_bytes = limit;
        self
    }

    /// Rejects any value that differs from its trimmed form.
    #[must_use]
    pub fn reject_surrounding_whitespace(mut self) -> Self {
        self.whitespace = Whitespace::RejectSurrounding;
        self
    }

    /// Treats a whitespace-only value as absent rather than as invalid.
    #[must_use]
    pub fn blank_is_missing(mut self) -> Self {
        self.blank_is_missing = true;
        self
    }

    /// Treats an explicitly empty value as if the setting were unset.
    #[must_use]
    pub fn empty_uses_default(mut self) -> Self {
        self.empty = EmptyValue::UseDefault;
        self
    }

    /// The canonical key.
    #[must_use]
    pub fn key(&self) -> &str {
        &self.key
    }

    /// The mandatory human-readable description.
    #[must_use]
    pub fn doc(&self) -> &str {
        &self.doc
    }

    /// The default value, or `None` when the field is required.
    #[must_use]
    pub fn default_value(&self) -> Option<&str> {
        self.default.as_deref()
    }

    /// Whether the process cannot start without an explicit value.
    #[must_use]
    pub fn is_required(&self) -> bool {
        self.default.is_none()
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

    /// The declared value-length ceiling.
    #[must_use]
    pub fn value_limit(&self) -> usize {
        self.maximum_bytes
    }

    pub(crate) fn whitespace(&self) -> Whitespace {
        self.whitespace
    }

    pub(crate) fn empty(&self) -> EmptyValue {
        self.empty
    }

    pub(crate) fn treats_blank_as_missing(&self) -> bool {
        self.blank_is_missing
    }

    /// Validates the declaration itself, independent of any loaded value.
    pub(crate) fn validate(&self, namespace: &str) -> FaultResult<()> {
        if !is_canonical_key(&self.key) {
            return Err(error::field_invalid(
                namespace,
                &self.key,
                "key must be lowercase alphanumeric with '_', '.', or '-' separators",
            ));
        }
        if self.doc.trim().is_empty() {
            return Err(error::field_invalid(
                namespace,
                &self.key,
                "documentation is mandatory so the configuration surface stays visible",
            ));
        }
        if self.doc.len() > MAX_DOC_BYTES {
            return Err(error::field_invalid(
                namespace,
                &self.key,
                "documentation exceeds its byte ceiling",
            ));
        }
        if self.maximum_bytes == 0 || self.maximum_bytes > MAX_VALUE_BYTES {
            return Err(error::field_invalid(
                namespace,
                &self.key,
                "value ceiling must be between 1 byte and the crate maximum",
            ));
        }
        if let Some(default) = &self.default
            && default.len() > self.maximum_bytes
        {
            return Err(error::field_invalid(
                namespace,
                &self.key,
                "default value exceeds the declared ceiling",
            ));
        }
        Ok(())
    }
}

/// Whether a key matches the canonical form shared with `libs/go/config`.
///
/// Lowercase so that one spelling of a key exists, and dotted so a key reads as
/// a path rather than as the environment variable it happens to be bound to.
/// The binding to an environment variable name lives in
/// [`EnvSource`](crate::EnvSource), never in the key.
#[must_use]
pub fn is_canonical_key(value: &str) -> bool {
    if value.is_empty() || value.len() > MAX_KEY_BYTES || value.trim() != value {
        return false;
    }
    for (index, character) in value.char_indices() {
        let alphanumeric = character.is_ascii_lowercase() || character.is_ascii_digit();
        if alphanumeric {
            continue;
        }
        if index > 0 && matches!(character, '_' | '.' | '-') {
            continue;
        }
        return false;
    }
    true
}
