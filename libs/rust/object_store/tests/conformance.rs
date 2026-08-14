use mindclade_object_store::{
    MemoryStore, ObjectPath, ObjectStore, PutCondition
};
use mindclade_bytes_io::ByteSize;
#[test]fn memory_roundtrip() {
    let s=MemoryStore::new();
    let p=ObjectPath::new("a/b").unwrap();
    s.put(&p, b"abc", PutCondition::CreateOnly).unwrap();
    assert_eq!(s.get(&p, ByteSize::new(3)).unwrap(), b"abc");
}
