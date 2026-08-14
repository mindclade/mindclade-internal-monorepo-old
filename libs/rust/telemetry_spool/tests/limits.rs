use mindclade_telemetry_spool::DeliveryBatch;
#[test]fn empty_batch_is_valid() {
    let b=DeliveryBatch::new(Vec::new(), 0).unwrap();
    assert!(b.highest_sequence().is_none());
}
