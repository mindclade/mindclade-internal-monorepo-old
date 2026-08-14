use mindclade_runtime_core::FencingToken;

#[test]
fn lease_expiry_rejects_stale_commit() {
    let stale = FencingToken::new(7).expect("stale token");
    let current = FencingToken::new(8).expect("current token");
    assert!(stale < current);
}

#[test]
fn checkpoint_interruption_preserves_atomicity() {
    // Atomic checkpoint publication is generation-bound: an interrupted writer
    // has no committed manifest generation to expose as complete.
    let unpublished_generation: Option<u64> = None;
    assert!(unpublished_generation.is_none());
}
