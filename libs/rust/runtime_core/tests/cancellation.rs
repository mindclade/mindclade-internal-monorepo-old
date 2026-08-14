use mindclade_runtime_core::CancellationToken;

#[test] fn cancellation_is_idempotent_and_preserves_first_reason() {
    let t=CancellationToken::new();
    assert!(t.cancel("shutdown"));
    assert!(!t.cancel("other"));
    assert!(t.is_cancelled());
    assert_eq!(t.reason().as_deref(), Some("shutdown"));
}
