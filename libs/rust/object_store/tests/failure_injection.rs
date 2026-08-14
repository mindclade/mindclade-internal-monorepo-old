use mindclade_object_store::retry::retryable;
use mindclade_faults::Code;
#[test]fn retry_classification_is_explicit() {
    assert!(retryable(Code::Unavailable));
    assert!(!retryable(Code::InvalidArgument));
}
