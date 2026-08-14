use mindclade_faults::{Fault, FaultResult};

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct NamedPipeEndpoint {
    path: String,
}
impl NamedPipeEndpoint {
    pub fn new(path: impl Into<String>) -> FaultResult<Self> {
        let path = path.into();
        let name = path.strip_prefix(r"\\.\pipe\").unwrap_or_default();
        if name.is_empty()
            || path.len() > 256
            || path.contains('\0')
            || name.contains('\\')
            || name == "."
            || name == ".."
        {
            return Err(Fault::invalid_argument("named pipe endpoint is invalid"));
        }
        Ok(Self { path })
    }
    #[must_use]
    pub fn path(&self) -> &str {
        &self.path
    }
}
