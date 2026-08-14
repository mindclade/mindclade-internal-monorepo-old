use mindclade_content_digest::hash_bytes;
use mindclade_data_stream::{
    Cursor, Shard, StreamPlan, resume::ResumePoint
};
use mindclade_identifiers::Name;
use mindclade_object_store::ObjectPath;
#[test]fn cursor_is_bound_to_plan() {
    let s=Shard {
        name: Name::new("s").unwrap(), path: ObjectPath::new("x").unwrap(), digest: hash_bytes(b"x"), size: 1, records: 1
    };
    let p=StreamPlan::new(Name::new("d").unwrap(), 0, 1, 1, 0, vec![s]).unwrap();
    let c=Cursor::start(p.plan_digest);
    assert!(ResumePoint::from_cursor(&c, &p).is_ok());
}
