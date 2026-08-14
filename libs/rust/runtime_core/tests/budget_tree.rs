use mindclade_runtime_core::{Budget,ResourceKind,ResourceVector};
#[test]
fn tree_reports_child_reservations_and_rejections(){
    let root=Budget::root("node",ResourceVector::new().set(ResourceKind::ResidentMemoryBytes,100));
    let child=Budget::child(root.clone(),"runtime-host",ResourceVector::new().set(ResourceKind::ResidentMemoryBytes,50));
    let _reservation=child.reserve(ResourceVector::new().set(ResourceKind::ResidentMemoryBytes,40)).expect("reserve");
    assert!(child.reserve(ResourceVector::new().set(ResourceKind::ResidentMemoryBytes,20)).is_err());
    let tree=root.tree_snapshot(); let host=tree.find("runtime-host").expect("host");
    assert_eq!(host.budget.reserved.get(ResourceKind::ResidentMemoryBytes),40); assert_eq!(host.budget.rejections,1);
}
