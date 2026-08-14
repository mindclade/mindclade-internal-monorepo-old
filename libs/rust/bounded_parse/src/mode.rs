//! Parser strictness. Recovery is explicit and never silently selected.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum ParseMode { Strict, Recovery }
impl ParseMode { #[must_use] pub const fn allows_recovery(self) -> bool { matches!(self, Self::Recovery) } }
