// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

//! Bounded monotonic counters and their Prometheus text rendering.
//!
//! Scope is deliberately monotonic counters only. Gauges, histograms, and
//! label dimensions are not modelled here; see this crate's README for why the
//! Rust tier renders the registry it already has rather than growing a second,
//! partial copy of `libs/go/observability`'s measurement model.

use std::collections::BTreeMap;
use std::fmt::Write as _;
use std::sync::{Arc, Mutex};

/// Prefix applied to every exported series, matching the fleet convention the
/// AI gateway established (`mindclade_ai_gateway_accepted_total`).
pub const METRIC_NAMESPACE: &str = "mindclade";

/// Suffix Prometheus convention requires on a monotonic counter.
pub const COUNTER_SUFFIX: &str = "_total";

/// Content type a `/metrics` handler must return for [`CounterRegistry::prometheus_text`].
pub const PROMETHEUS_CONTENT_TYPE: &str = "text/plain; version=0.0.4; charset=utf-8";

/// Validates a counter name against the rule `libs/go/observability`'s
/// `validMetricName` applies to `Measurement.Name`: lowercase ASCII, bounded
/// length, `[a-z0-9._]` only, a letter first, no trailing separator, and no
/// two adjacent separators.
///
/// Keeping the two tiers on one rule is the point. A name legal in Go and
/// illegal in Rust (or vice versa) makes the same fleet metric unnameable from
/// one side, which is how a Go dashboard and a Rust service end up disagreeing
/// about what a counter is called.
#[must_use]
pub fn valid_counter_name(name: &str) -> bool {
    if name.is_empty() || name.len() > CounterRegistry::MAX_NAME_LEN {
        return false;
    }
    let bytes = name.as_bytes();
    let mut previous_separator = false;
    for (index, byte) in bytes.iter().copied().enumerate() {
        let letter = byte.is_ascii_lowercase();
        let digit = byte.is_ascii_digit();
        let separator = byte == b'.' || byte == b'_';
        if !letter && !digit && !separator {
            return false;
        }
        if index == 0 && !letter {
            return false;
        }
        if index == bytes.len() - 1 && separator {
            return false;
        }
        if separator && previous_separator {
            return false;
        }
        previous_separator = separator;
    }
    true
}

/// Maps a registry counter name onto its Prometheus series name.
///
/// `.` is the separator this registry and the Go tier use; Prometheus permits
/// only `[a-zA-Z0-9_:]` in a metric name, so `.` folds to `_`.
///
/// The fold is **not** injective — `stage.failed` and `stage_failed` are both
/// legal counter names and both render as `mindclade_stage_failed_total`.
/// [`CounterRegistry`] therefore refuses to admit a name whose series name is
/// already claimed, rather than letting two counters interleave samples into
/// one series where the collision is invisible at the scrape.
#[must_use]
pub fn prometheus_series_name(counter: &str) -> String {
    let mut series =
        String::with_capacity(METRIC_NAMESPACE.len() + 1 + counter.len() + COUNTER_SUFFIX.len());
    series.push_str(METRIC_NAMESPACE);
    series.push('_');
    for character in counter.chars() {
        series.push(if character == '.' { '_' } else { character });
    }
    series.push_str(COUNTER_SUFFIX);
    series
}

/// Bounded registry of monotonic counters.
///
/// Cardinality is capped. The registry backs a `/metrics` body, so an
/// unbounded name space is an unbounded response body and an unbounded
/// server-side map fed by whatever the caller passes as a name.
#[derive(Clone, Debug, Default)]
pub struct CounterRegistry {
    inner: Arc<Mutex<BTreeMap<String, u64>>>,
}

impl CounterRegistry {
    /// Maximum distinct counter names one registry will hold.
    pub const MAX_COUNTERS: usize = 256;
    /// Maximum counter name length, matching `observability.MaximumMetricNameLength`.
    pub const MAX_NAME_LEN: usize = 128;

    /// Declares a counter at zero without incrementing it, and without
    /// disturbing a counter that is already accumulating.
    ///
    /// Prometheus cannot distinguish "no events yet" from "instrumentation
    /// missing" when a series is simply absent, and `rate()` over a series that
    /// springs into existence mid-window is wrong at its first sample. A
    /// service should register its full counter set at construction.
    ///
    /// Returns false for an invalid name, a series-name collision, or a full
    /// registry.
    #[must_use]
    pub fn register(&self, name: &str) -> bool {
        let Some(mut metrics) = self.admit(name) else {
            return false;
        };
        metrics.entry(name.to_owned()).or_insert(0);
        true
    }

    /// Adds to a bounded monotonic counter. Returns false for an invalid name,
    /// a series-name collision, a full registry, or an addition that would
    /// overflow; metrics must never silently wrap or saturate.
    #[must_use]
    pub fn add(&self, name: &str, value: u64) -> bool {
        let Some(mut metrics) = self.admit(name) else {
            return false;
        };
        let entry = metrics.entry(name.to_owned()).or_default();
        let Some(next) = entry.checked_add(value) else {
            return false;
        };
        *entry = next;
        true
    }

    /// Takes the registry lock, having established that `name` may occupy a
    /// slot: it is well formed, and either already present or admissible
    /// without breaching the cardinality cap or colliding with an existing
    /// series name.
    fn admit(&self, name: &str) -> Option<std::sync::MutexGuard<'_, BTreeMap<String, u64>>> {
        if !valid_counter_name(name) {
            return None;
        }
        let metrics = self
            .inner
            .lock()
            .unwrap_or_else(std::sync::PoisonError::into_inner);
        if metrics.contains_key(name) {
            return Some(metrics);
        }
        if metrics.len() >= Self::MAX_COUNTERS {
            return None;
        }
        let series = prometheus_series_name(name);
        if metrics
            .keys()
            .any(|existing| prometheus_series_name(existing) == series)
        {
            return None;
        }
        Some(metrics)
    }

    #[must_use]
    pub fn snapshot(&self) -> BTreeMap<String, u64> {
        self.inner
            .lock()
            .unwrap_or_else(std::sync::PoisonError::into_inner)
            .clone()
    }

    #[must_use]
    pub fn len(&self) -> usize {
        self.inner
            .lock()
            .unwrap_or_else(std::sync::PoisonError::into_inner)
            .len()
    }

    #[must_use]
    pub fn is_empty(&self) -> bool {
        self.len() == 0
    }

    /// Renders every counter as Prometheus text exposition format 0.0.4.
    ///
    /// Deterministic: the backing map is ordered, so two scrapes of an
    /// unchanged registry are byte-identical. Bounded: at most
    /// [`Self::MAX_COUNTERS`] series of at most [`Self::MAX_NAME_LEN`] name
    /// bytes each, so the body has a fixed ceiling regardless of traffic.
    #[must_use]
    pub fn prometheus_text(&self) -> String {
        let snapshot = self.snapshot();
        let mut output = String::new();
        for (name, value) in &snapshot {
            let series = prometheus_series_name(name);
            // Writing into a String is infallible; the result is discarded
            // rather than unwrapped so the render stays panic-free.
            let _ = writeln!(output, "# TYPE {series} counter");
            let _ = writeln!(output, "{series} {value}");
        }
        output
    }
}
