//! Stable source position used in parse diagnostics.
#[derive(Clone, Copy, Debug, Default, Eq, PartialEq)]
pub struct Location {
    pub offset: usize,
    pub line: usize,
    pub column: usize
}

impl Location {
    #[must_use] pub const fn start() -> Self {
        Self {
            offset: 0, line: 1, column: 1
        }
    }
    #[must_use] pub const fn at(offset: usize, line: usize, column: usize) -> Self {
        Self {
            offset, line, column
        }
    }
}
