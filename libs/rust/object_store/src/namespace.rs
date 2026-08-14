use crate::ObjectPath;
use mindclade_faults::{
    Code, Fault, FaultResult
};

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct Namespace {
    prefix: ObjectPath
}

impl Namespace {
    pub fn new(prefix: ObjectPath) -> Self {
        Self {
            prefix
        }
    }
    #[must_use]pub fn prefix(&self) -> &ObjectPath {
        &self.prefix
    }
    pub fn qualify(&self, relative: &str) -> FaultResult<ObjectPath> {
        if relative.is_empty() {
            return Err(Fault::invalid_argument("relative object path is empty"));
        }
        ObjectPath::new(format!("{}/{}", self.prefix.as_str(), relative)).map_err(|e|Fault::new(Code::InvalidArgument, e.to_string()))
    }
    pub fn contains(&self, path: &ObjectPath) -> bool {
        path.as_str()==self.prefix.as_str()||path.as_str().strip_prefix(self.prefix.as_str()).is_some_and(|s|s.starts_with('/'))
    }
}
