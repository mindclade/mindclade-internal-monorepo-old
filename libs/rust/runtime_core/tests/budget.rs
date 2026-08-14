use mindclade_runtime_core::{
    BudgetHierarchy, ResourceKind, ResourceLimits
};

#[test] fn hierarchy_accounts_and_releases() {
    let limits=ResourceLimits::new().limit(ResourceKind::ResidentMemoryBytes, 100).into_vector();
    let h=BudgetHierarchy::new(limits.clone());
    let svc=h.service("svc", limits);
    {
        let _r=svc.reserve(ResourceLimits::new().limit(ResourceKind::ResidentMemoryBytes, 60).into_vector()).unwrap();
        let(_, used)=svc.snapshot();
        assert_eq!(used.get(ResourceKind::ResidentMemoryBytes), 60);
    }
    let(_, used)=svc.snapshot();
    assert_eq!(used.get(ResourceKind::ResidentMemoryBytes), 0);
}

#[test] fn hierarchy_rejects_aggregate_overcommit() {
    let limits=ResourceLimits::new().limit(ResourceKind::CpuThreads, 2).into_vector();
    let h=BudgetHierarchy::new(limits.clone());
    let svc=h.service("svc", limits);
    let _one=svc.reserve(ResourceLimits::new().limit(ResourceKind::CpuThreads, 2).into_vector()).unwrap();
    assert!(svc.reserve(ResourceLimits::new().limit(ResourceKind::CpuThreads, 1).into_vector()).is_err());
}
