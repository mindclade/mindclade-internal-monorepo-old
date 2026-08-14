use mindclade_servicekit::{
    Lifecycle, LifecycleState
};
#[test]fn lifecycle_requires_drain_or_stop() {
    let l=Lifecycle::new();
    l.transition(LifecycleState::Starting).unwrap();
    l.transition(LifecycleState::Running).unwrap();
    l.transition(LifecycleState::Draining).unwrap();
    l.transition(LifecycleState::Stopping).unwrap();
    l.transition(LifecycleState::Stopped).unwrap();
}
