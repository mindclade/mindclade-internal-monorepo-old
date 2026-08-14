//! Typed error contracts shared by Mindclade Rust components.
#![forbid(unsafe_code)]
mod code;
mod context;
mod fault;
pub mod retry;
pub mod wire;
pub use code::Code;
pub use context::{
    Context, ContextValue
};
pub use fault::{
    Fault, FaultResult, RetryHint
};
pub use wire::{
    WireContext, WireFault
};
