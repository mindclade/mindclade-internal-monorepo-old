use crate::TraceContext;
use mindclade_faults::{
    Fault, FaultResult
};

#[derive(Clone, Debug)]
pub struct SpanContext {
    pub trace: TraceContext,
    pub name: String
}

impl SpanContext {
    pub fn new(trace: TraceContext, name: impl Into<String>) -> FaultResult<Self> {
        let name=name.into();
        if name.is_empty()||name.len()>256 {
            return Err(Fault::invalid_argument("span name is invalid"));
        }
        Ok(Self {
            trace, name
        })
    }
}
