// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

//! Bounded diagnostic sink for recovery-mode parsing.
//!
//! This is the one structure in the crate that grows per *defect* rather than
//! per input byte, which is exactly why it needs a ceiling of its own: a hostile
//! file is malformed everywhere, so one diagnostic per malformed construct turns
//! a bounded parse of bounded input into an unbounded retained allocation of
//! `String` messages. It used to be an uncapped `Vec` behind an infallible push,
//! and `Diagnostic::validate` — which bounds the code and message lengths — was
//! never called on anything.
//!
//! Retention is now hard-bounded at `Limits::maximum_metadata_entries`
//! diagnostics, each clamped to `MAXIMUM_MESSAGE_BYTES`, so the sink holds at
//! most `maximum_metadata_entries * 4 KiB` no matter what the input looks like.

use crate::{Diagnostic, Limits, ParseMode};
use mindclade_faults::FaultResult;

#[derive(Clone, Debug)]
pub struct Recovery {
    mode: ParseMode,
    maximum_diagnostics: usize,
    suppressed: u64,
    diagnostics: Vec<Diagnostic>,
}

impl Recovery {
    /// Binds a sink to the same `Limits` the parse is bounded by.
    ///
    /// There is deliberately no limits-free constructor: one that fell back to
    /// `Limits::default()` would silently retain 1024 diagnostics for a caller
    /// who had configured a far tighter metadata budget, which is fail-open for
    /// the ceiling this type exists to enforce.
    pub fn new(mode: ParseMode, limits: Limits) -> FaultResult<Self> {
        Ok(Self {
            mode,
            maximum_diagnostics: limits.validate()?.maximum_metadata_entries,
            suppressed: 0,
            diagnostics: Vec::new(),
        })
    }

    #[must_use]
    pub const fn mode(&self) -> ParseMode {
        self.mode
    }

    /// Inclusive ceiling on retained diagnostics.
    #[must_use]
    pub const fn maximum_diagnostics(&self) -> usize {
        self.maximum_diagnostics
    }

    /// Diagnostics dropped because the ceiling was already reached.
    ///
    /// Non-zero means [`Self::diagnostics`] is an incomplete report. Callers that
    /// present diagnostics to a human must say so rather than implying the list
    /// is exhaustive.
    #[must_use]
    pub const fn suppressed(&self) -> u64 {
        self.suppressed
    }

    /// Records one diagnostic, dropping it once the ceiling is reached.
    ///
    /// Truncation rather than failure is the point of recovery mode: a parse
    /// that aborted on diagnostic 1025 would be strict mode with extra steps.
    /// The loss is never silent — it is counted in [`Self::suppressed`].
    ///
    /// An oversized *message* is clamped for the same reason. A reporter
    /// naturally quotes the construct it rejected, and a hostile line may run to
    /// `maximum_line_bytes`; returning an error there would abort the parse
    /// through the caller's `?` and degrade recovery into strict mode under
    /// exactly the adversarial input recovery exists to survive.
    ///
    /// The remaining error is genuinely not a data condition: it means the
    /// *parser* declared a `code` outside its bounds, which is a constant chosen
    /// by the parser author and so a bug worth surfacing rather than storing.
    ///
    /// Strict mode discards diagnostics and always succeeds, as before.
    pub fn record(&mut self, diagnostic: Diagnostic) -> FaultResult<()> {
        if !self.mode.allows_recovery() {
            return Ok(());
        }
        let diagnostic = diagnostic.truncated();
        diagnostic.validate()?;
        if self.diagnostics.len() >= self.maximum_diagnostics {
            // Written as `if let Some` rather than `saturating_add` (which
            // `tools/analysis/check_rust_arithmetic.py` rejects) or
            // `checked_add(..).unwrap_or(u64::MAX)` (which clippy rewrites back
            // into `saturating_add`). A bounded input cannot produce u64
            // diagnostics; if one somehow did, the count has stopped being
            // meaningful and freezing it is better than wrapping it.
            if let Some(next) = self.suppressed.checked_add(1) {
                self.suppressed = next;
            }
            return Ok(());
        }
        self.diagnostics.push(diagnostic);
        Ok(())
    }

    #[must_use]
    pub fn diagnostics(&self) -> &[Diagnostic] {
        &self.diagnostics
    }

    #[must_use]
    pub fn into_diagnostics(self) -> Vec<Diagnostic> {
        self.diagnostics
    }
}
