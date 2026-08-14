//! Root-confined atomic filesystem publication and verified reads.
#![forbid(unsafe_code)]

mod path;
mod store;
pub use path::{RelativePath, RelativePathError};
pub use store::{AtomicFileStore, PublishedFile};
