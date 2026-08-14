pub use crate::spec::ByteRange;

impl ByteRange {
    #[must_use]pub fn intersects(self, other: Self) -> bool {
        self.start()<other.end()&&other.start()<self.end()
    }
    pub fn intersection(self, other: Self) -> mindclade_faults::FaultResult<Option<Self>> {
        let start=self.start().max(other.start());
        let end=self.end().min(other.end());
        if start>=end {
            return Ok(None);
        }
        Ok(Some(Self::new(start, end-start)?))
    }
}
