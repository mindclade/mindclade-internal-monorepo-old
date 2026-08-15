// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

use crate::TraceContext;
use mindclade_faults::{Fault, FaultResult};

pub fn encode_traceparent(ctx: &TraceContext) -> FaultResult<String> {
    if ctx.trace_id.len() != 32
        || ctx.span_id.len() != 16
        || !ctx.trace_id.bytes().all(|b| b.is_ascii_hexdigit())
        || !ctx.span_id.bytes().all(|b| b.is_ascii_hexdigit())
    {
        return Err(Fault::invalid_argument("trace context is invalid"));
    }
    Ok(format!(
        "00-{}-{}-{:02x}",
        ctx.trace_id.to_ascii_lowercase(),
        ctx.span_id.to_ascii_lowercase(),
        u8::from(ctx.sampled)
    ))
}

pub fn decode_traceparent(value: &str) -> FaultResult<TraceContext> {
    let p: Vec<_> = value.split('-').collect();
    if p.len() != 4 || p[0] != "00" || p[1].len() != 32 || p[2].len() != 16 {
        return Err(Fault::invalid_argument("traceparent is invalid"));
    }
    Ok(TraceContext {
        trace_id: p[1].to_owned(),
        span_id: p[2].to_owned(),
        sampled: p[3] == "01",
    })
}
