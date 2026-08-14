use mindclade_bytes_io::ByteRange;
use mindclade_object_store::range::RangePolicy;
use mindclade_bytes_io::ByteSize;
#[test]fn range_policy_is_bounded() {
    let p=RangePolicy {
        maximum_read: ByteSize::new(10)
    };
    assert!(p.validate(ByteRange::new(0, 10).unwrap()).is_ok());
    assert!(p.validate(ByteRange::new(0, 11).unwrap()).is_err());
}
