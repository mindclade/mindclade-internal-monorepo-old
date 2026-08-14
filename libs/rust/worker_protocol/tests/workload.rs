use mindclade_worker_protocol::{WorkloadEnvelope,WorkloadKind};
#[test] fn workload_types_are_public(){let _=core::mem::size_of::<WorkloadEnvelope>();let _=WorkloadKind::Ingestion;}
