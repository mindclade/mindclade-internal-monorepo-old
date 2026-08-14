use mindclade_checkpoint_io::StagingBudget;
use mindclade_bytes_io::ByteSize;
#[test]fn staging_budget_rejects_overcommit() {
    let b=StagingBudget::new(ByteSize::new(10));
    let _r=b.reserve(ByteSize::new(8)).unwrap();
    assert!(b.reserve(ByteSize::new(3)).is_err());
}
