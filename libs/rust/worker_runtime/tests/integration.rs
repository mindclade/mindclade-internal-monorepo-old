use mindclade_runtime_core::{
    Budget, FencingToken, ResourceKind, ResourceVector
};
use mindclade_worker_runtime::WorkerRuntime;

#[test] fn invalid_transition_is_rejected() {
    let b=Budget::root("node", ResourceVector::new().set(ResourceKind::ResidentMemoryBytes, 1024));
    let w=WorkerRuntime::new(b);
    assert!(w.run().is_err());
    assert!(w.start().is_ok());
    assert!(w.run().is_err());
    let _=FencingToken::new(1);
}
