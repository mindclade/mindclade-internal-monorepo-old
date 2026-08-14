use mindclade_bytes_io::ByteSize;
use mindclade_content_digest::hash_bytes;
use mindclade_data_stream::{
    Cursor, Shard, StreamPlan
};
use mindclade_identifiers::Name;
use mindclade_object_store::ObjectPath;

#[test]
fn plans_and_cursors_are_reproducible() {
    let dataset = Name::new("datasets/pretraining");
    let name = Name::new("shards/0000");
    let path = ObjectPath::new("datasets/pretraining/shard-0000.mcrd");
    assert!(dataset.is_ok() && name.is_ok() && path.is_ok());
    if let (Ok(dataset), Ok(name), Ok(path)) = (dataset, name, path) {
        let shard = Shard {
            name, path, digest: hash_bytes(b"records"), size: 7, records: 1
        };
        let first = StreamPlan::new(dataset.clone(), 0, 42, 1, 0, vec![shard.clone()]);
        let second = StreamPlan::new(dataset, 0, 42, 1, 0, vec![shard]);
        assert_eq!(first.as_ref().ok().map(|plan| plan.plan_digest), second.as_ref().ok().map(|plan| plan.plan_digest));
        if let Ok(plan) = first {
            let cursor = Cursor::start(plan.plan_digest);
            let bytes = cursor.encode();
            assert!(bytes.is_ok());
            if let Ok(bytes) = bytes {
                assert_eq!(Cursor::decode(&bytes).ok(), Some(cursor));
            }
        }
    }
    let _ = ByteSize::new(1);
}
