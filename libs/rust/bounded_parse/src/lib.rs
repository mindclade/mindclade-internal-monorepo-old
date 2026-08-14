//! Allocation- and input-bounded parsing primitives for untrusted scientific bytes.
#![forbid(unsafe_code)]
mod budget;
mod cursor;
mod diagnostic;
mod limits;
mod location;
mod mode;
mod recovery;
mod source;
pub use budget::AllocationBudget;
pub use cursor::Cursor;
pub use diagnostic::Diagnostic;
pub use limits::Limits;
pub use location::Location;
pub use mode::ParseMode;
pub use recovery::Recovery;
pub use source::Source;
