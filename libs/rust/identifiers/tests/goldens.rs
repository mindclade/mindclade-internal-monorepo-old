use mindclade_identifiers::{
    ResourceId, ResourceKind
};
use std::str::FromStr;

#[test] fn resource_id_is_canonical() {
    let id=ResourceId::from_str("run_01arZ3NDEKTSV4RRFFQ69G5FAV".to_ascii_lowercase().as_str());
    assert!(id.is_err(), "fixture deliberately rejects noncanonical mixed schema");
}

#[test] fn kind_validation() {
    assert!(ResourceKind::parse("runtime_host").is_ok());
    assert!(ResourceKind::parse("RuntimeHost").is_err());
}
