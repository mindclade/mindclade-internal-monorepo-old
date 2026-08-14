use mindclade_content_digest::hash_bytes;
use mindclade_ipc::shared_memory::SharedMemoryDescriptor;
#[test]fn descriptor_has_explicit_lifetime() {
    let d=SharedMemoryDescriptor::new("x".into(), 1, 3, hash_bytes(b"abc"), "worker".into(), 99, "shm://x".into(), 0).unwrap();
    assert_eq!(d.descriptor.lease_expires_unix_millis, 99);
}
