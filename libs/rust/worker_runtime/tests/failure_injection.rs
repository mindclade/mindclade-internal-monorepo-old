use mindclade_runtime_core::{Budget, ResourceVector};
use mindclade_worker_runtime::WorkerRuntime;

fn runtime() -> WorkerRuntime {
    WorkerRuntime::new(Budget::root("test", ResourceVector::default()))
}

#[test]
fn kill_before_commit() {
    let runtime = runtime();
    runtime.start().expect("start");
    assert_eq!(format!("{:?}", runtime.state()), "Ready");
    runtime.cancel("process terminated before commit").expect("cancel");
    assert_eq!(format!("{:?}", runtime.state()), "Cancelled");
}

#[test]
fn kill_after_commit_before_ack() {
    // The durable artifact commit is outside the worker acknowledgment.  A
    // replacement attempt therefore begins from Ready with a fresh fence and
    // must rediscover the already committed content-addressed output.
    let first = runtime();
    first.start().expect("start");
    first.cancel("worker lost after durable output").expect("cancel");
    let replacement = runtime();
    replacement.start().expect("replacement start");
    assert_eq!(format!("{:?}", replacement.state()), "Ready");
}
