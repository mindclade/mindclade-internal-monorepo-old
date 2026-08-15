// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

//! Bounded, redacted node diagnostics suitable for incident artifacts.

use mindclade_faults::{Code, Fault, FaultResult};
use mindclade_runtime_core::{BudgetTreeSnapshot, ResourceVector};
use std::collections::BTreeMap;

const DIAGNOSTIC_MAGIC: &[u8; 8] = b"MCDIAG01";
pub const DEFAULT_MAXIMUM_DIAGNOSTIC_BYTES: usize = 8 * 1024 * 1024;

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct DiagnosticProcessExit {
    pub component: String,
    pub exit_code: Option<i32>,
    pub when_unix_millis: u64,
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct NodeDiagnosticsBundle {
    pub node_id: String,
    pub runtime_version: String,
    pub generated_unix_millis: u64,
    pub budget: BudgetTreeSnapshot,
    pub active_ticket_ids: Vec<String>,
    pub process_exits: Vec<DiagnosticProcessExit>,
    pub cache_bytes: u64,
    pub telemetry_spool_bytes: u64,
    pub attributes: BTreeMap<String, String>,
}

impl NodeDiagnosticsBundle {
    pub fn validate(&self) -> FaultResult<()> {
        if self.node_id.is_empty()
            || self.node_id.len() > 256
            || self.runtime_version.is_empty()
            || self.runtime_version.len() > 128
            || self.generated_unix_millis == 0
            || self.active_ticket_ids.len() > 4_096
            || self.process_exits.len() > 1_024
            || self.attributes.len() > 128
        {
            return Err(Fault::new(
                Code::ResourceExhausted,
                "diagnostic bundle exceeds bounds",
            ));
        }
        if self
            .active_ticket_ids
            .iter()
            .any(|value| value.is_empty() || value.len() > 256)
        {
            return Err(Fault::invalid_argument(
                "diagnostic ticket identifier is invalid",
            ));
        }
        if self.process_exits.iter().any(|exit| {
            exit.component.is_empty() || exit.component.len() > 256 || exit.when_unix_millis == 0
        }) {
            return Err(Fault::invalid_argument(
                "diagnostic process exit is invalid",
            ));
        }
        if self.attributes.iter().any(|(key, value)| {
            key.is_empty() || key.len() > 128 || value.len() > 1_024 || is_sensitive(key)
        }) {
            return Err(Fault::invalid_argument(
                "diagnostic attributes contain invalid or sensitive data",
            ));
        }
        Ok(())
    }

    /// Deterministic versioned representation used for the incident artifact.
    /// No raw payload/model bytes are accepted by this type.
    pub fn encode(&self, maximum_bytes: usize) -> FaultResult<Vec<u8>> {
        self.validate()?;
        if maximum_bytes == 0 || maximum_bytes > DEFAULT_MAXIMUM_DIAGNOSTIC_BYTES {
            return Err(Fault::invalid_argument(
                "diagnostic maximum byte bound is invalid",
            ));
        }
        let mut output = Vec::with_capacity(4096.min(maximum_bytes));
        output.extend_from_slice(DIAGNOSTIC_MAGIC);
        push_string(&mut output, &self.node_id)?;
        push_string(&mut output, &self.runtime_version)?;
        output.extend_from_slice(&self.generated_unix_millis.to_be_bytes());
        output.extend_from_slice(&self.cache_bytes.to_be_bytes());
        output.extend_from_slice(&self.telemetry_spool_bytes.to_be_bytes());
        push_len(&mut output, self.active_ticket_ids.len())?;
        for ticket in &self.active_ticket_ids {
            push_string(&mut output, ticket)?;
        }
        push_len(&mut output, self.process_exits.len())?;
        for exit in &self.process_exits {
            push_string(&mut output, &exit.component)?;
            match exit.exit_code {
                Some(value) => {
                    output.push(1);
                    output.extend_from_slice(&value.to_be_bytes());
                }
                None => output.push(0),
            }
            output.extend_from_slice(&exit.when_unix_millis.to_be_bytes());
        }
        push_len(&mut output, self.attributes.len())?;
        for (key, value) in &self.attributes {
            push_string(&mut output, key)?;
            push_string(&mut output, value)?;
        }
        push_budget_tree(&mut output, &self.budget)?;
        if output.len() > maximum_bytes {
            return Err(Fault::new(
                Code::ResourceExhausted,
                "diagnostic artifact exceeds configured byte bound",
            ));
        }
        Ok(output)
    }

    pub fn estimated_bytes(&self) -> FaultResult<u64> {
        let encoded = self.encode(DEFAULT_MAXIMUM_DIAGNOSTIC_BYTES)?;
        u64::try_from(encoded.len())
            .map_err(|_| Fault::new(Code::OutOfRange, "diagnostic size exceeds u64"))
    }
}

fn push_budget_tree(output: &mut Vec<u8>, tree: &BudgetTreeSnapshot) -> FaultResult<()> {
    push_string(output, &tree.budget.name)?;
    push_resources(output, &tree.budget.limits)?;
    push_resources(output, &tree.budget.reserved)?;
    push_resources(output, &tree.budget.used_estimate)?;
    output.extend_from_slice(&tree.budget.waiters.to_be_bytes());
    output.extend_from_slice(&tree.budget.rejections.to_be_bytes());
    output.push(u8::from(tree.budget.corrupted));
    push_len(output, tree.children.len())?;
    for child in &tree.children {
        push_budget_tree(output, child)?;
    }
    Ok(())
}

fn push_resources(output: &mut Vec<u8>, resources: &ResourceVector) -> FaultResult<()> {
    let values: Vec<_> = resources.iter().collect();
    push_len(output, values.len())?;
    for (kind, value) in values {
        push_string(output, &format!("{kind:?}"))?;
        output.extend_from_slice(&value.to_be_bytes());
    }
    Ok(())
}

fn push_string(output: &mut Vec<u8>, value: &str) -> FaultResult<()> {
    push_len(output, value.len())?;
    output.extend_from_slice(value.as_bytes());
    Ok(())
}

fn push_len(output: &mut Vec<u8>, value: usize) -> FaultResult<()> {
    let value = u32::try_from(value)
        .map_err(|_| Fault::new(Code::OutOfRange, "diagnostic length exceeds u32"))?;
    output.extend_from_slice(&value.to_be_bytes());
    Ok(())
}

fn is_sensitive(key: &str) -> bool {
    let lower = key.to_ascii_lowercase();
    [
        "secret",
        "token",
        "password",
        "credential",
        "private_key",
        "raw_input",
        "model_weight",
    ]
    .iter()
    .any(|needle| lower.contains(needle))
}
