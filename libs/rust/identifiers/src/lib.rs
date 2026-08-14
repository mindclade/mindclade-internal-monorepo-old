//! Validated names, resource kinds, digests, and canonical resource identifiers.
#![forbid(unsafe_code)]
pub mod digest;
pub mod id;
pub mod kind;
mod name;
mod resource_id;
pub mod resource_version;
pub use kind::ResourceKind;
pub use name::{
    Name, NameError
};
pub use resource_id::{
    ResourceId, ResourceIdError
};
