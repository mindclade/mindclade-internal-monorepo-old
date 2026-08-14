use crate::LocalStore;
use mindclade_faults::FaultResult;
use std::path::Path;
pub fn open(root: &Path) -> FaultResult<LocalStore> {
    LocalStore::new(root)
}
