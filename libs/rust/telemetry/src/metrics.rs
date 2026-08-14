use std::collections::BTreeMap;
use std::sync::{Arc, Mutex};

#[derive(Clone, Debug, Default)]
pub struct CounterRegistry {
    inner: Arc<Mutex<BTreeMap<String, u64>>>,
}
impl CounterRegistry {
    /// Adds to a bounded monotonic counter. Returns false for an invalid name or
    /// if the counter would overflow; metrics must never silently wrap/saturate.
    pub fn add(&self, name: &str, value: u64) -> bool {
        if name.is_empty() || name.len() > 128 {
            return false;
        }
        let mut metrics = self.inner.lock().unwrap_or_else(|poisoned| poisoned.into_inner());
        let entry = metrics.entry(name.to_owned()).or_default();
        let Some(next) = entry.checked_add(value) else { return false; };
        *entry = next;
        true
    }
    #[must_use]
    pub fn snapshot(&self) -> BTreeMap<String, u64> {
        self.inner.lock().unwrap_or_else(|poisoned| poisoned.into_inner()).clone()
    }
}
