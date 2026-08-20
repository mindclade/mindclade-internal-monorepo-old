// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

use mindclade_node_agent::{NodeAgentConfig, NodeHealth, ReferenceCache};
use mindclade_runtime_core::{ResourceKind, ResourceVector};

#[test]
fn validates_node_limits_and_drain() {
    let resources = ResourceVector::new()
        .set(ResourceKind::ResidentMemoryBytes, 1 << 30)
        .set(ResourceKind::LocalDiskBytes, 1 << 30)
        .set(ResourceKind::OpenFileDescriptors, 1024)
        .set(ResourceKind::CpuThreads, 8);
    let config = NodeAgentConfig {
        node_resources: resources,
        maximum_reference_cache_bytes: 1 << 30,
        maximum_tool_output_bytes: 1 << 20,
        maximum_children: 8,
        tool_poll_interval: std::time::Duration::from_millis(10),
    };
    assert!(config.validate().is_ok());
    assert!(ReferenceCache::new(1 << 30).is_ok());
    let health = NodeHealth::new();
    health.drain();
    assert!(!health.begin());
}

#[test]
fn reference_cache_rejects_accounting_overflow() {
    use mindclade_content_digest::Digest;
    use mindclade_node_agent::ReferenceSnapshot;
    use std::path::PathBuf;
    let cache = ReferenceCache::new(u64::MAX).expect("cache");
    cache
        .activate(ReferenceSnapshot {
            digest: Digest::from_bytes([1; 32]),
            root: PathBuf::from("/refs/a"),
            bytes: u64::MAX,
            activated_unix_millis: 1,
        })
        .expect("first snapshot");
    let error = cache.activate(ReferenceSnapshot {
        digest: Digest::from_bytes([2; 32]),
        root: PathBuf::from("/refs/b"),
        bytes: 1,
        activated_unix_millis: 2,
    });
    assert!(error.is_err());
}

#[test]
fn reference_cache_rejects_lexical_path_escape() {
    use mindclade_content_digest::Digest;
    use mindclade_node_agent::ReferenceSnapshot;
    use std::path::{Path, PathBuf};

    let digest = Digest::from_bytes([3; 32]);
    let cache = ReferenceCache::new(1024).expect("cache");
    cache
        .activate(ReferenceSnapshot {
            digest,
            root: PathBuf::from("/refs/snapshot"),
            bytes: 1,
            activated_unix_millis: 1,
        })
        .expect("snapshot");
    assert!(cache.contains_path(digest, Path::new("/refs/snapshot/data.bin")));
    assert!(!cache.contains_path(digest, Path::new("/refs/snapshot/../secret")));
    assert!(!cache.contains_path(digest, Path::new("relative/data.bin")));
}

#[test]
fn tool_request_enforces_hermetic_resource_bounds() {
    use mindclade_node_agent::{MAXIMUM_TOOL_OUTPUT_BYTES, MAXIMUM_TOOL_TIMEOUT, ToolRequest};
    use std::collections::BTreeMap;
    use std::time::Duration;

    let mut request = ToolRequest {
        executable: "/bin/true".to_owned(),
        arguments: Vec::new(),
        environment: BTreeMap::new(),
        timeout: Duration::from_secs(1),
        maximum_output_bytes: 1024,
    };
    assert!(request.validate().is_ok());
    request.executable = "true".to_owned();
    assert!(request.validate().is_err());
    request.executable = "/bin/true".to_owned();
    request.maximum_output_bytes = MAXIMUM_TOOL_OUTPUT_BYTES + 1;
    assert!(request.validate().is_err());
    request.maximum_output_bytes = 1024;
    request.timeout = MAXIMUM_TOOL_TIMEOUT + Duration::from_millis(1);
    assert!(request.validate().is_err());
}

#[cfg(unix)]
#[test]
fn tool_runner_truncates_output_and_reaps_timed_out_children() {
    use mindclade_node_agent::{ProcessSupervisor, ToolRequest, ToolRunner};
    use std::collections::BTreeMap;
    use std::sync::Arc;
    use std::time::Duration;

    let supervisor = Arc::new(ProcessSupervisor::new(2).expect("supervisor"));
    let runner = ToolRunner::new(supervisor.clone(), Duration::from_millis(1)).expect("runner");
    let output = runner
        .run(&ToolRequest {
            executable: "/bin/sh".to_owned(),
            arguments: vec![
                "-c".to_owned(),
                "printf abcdef; printf error-output >&2".to_owned(),
            ],
            environment: BTreeMap::new(),
            timeout: Duration::from_secs(1),
            maximum_output_bytes: 4,
        })
        .expect("tool should complete");
    assert_eq!(output.stdout, b"abcd");
    assert_eq!(output.stderr, b"erro");
    assert!(output.stdout_truncated);
    assert!(output.stderr_truncated);
    assert!(!output.timed_out);

    let timed_out = runner
        .run(&ToolRequest {
            executable: "/bin/sh".to_owned(),
            arguments: vec!["-c".to_owned(), "while :; do :; done".to_owned()],
            environment: BTreeMap::new(),
            timeout: Duration::from_millis(20),
            maximum_output_bytes: 4,
        })
        .expect("timeout should be a bounded outcome");
    assert!(timed_out.timed_out);
    assert_eq!(supervisor.active(), 0);
}

#[cfg(unix)]
#[test]
fn tool_runner_enforces_aggregate_retained_output_budget() {
    use mindclade_faults::Code;
    use mindclade_node_agent::{ProcessSupervisor, ToolRequest, ToolRunner};
    use std::collections::BTreeMap;
    use std::sync::Arc;
    use std::time::Duration;

    let supervisor = Arc::new(ProcessSupervisor::new(2).expect("supervisor"));
    let runner =
        ToolRunner::with_output_budget(supervisor, Duration::from_millis(1), 8).expect("runner");
    let first = runner
        .run(&ToolRequest {
            executable: "/bin/sh".to_owned(),
            arguments: vec!["-c".to_owned(), "printf 12345".to_owned()],
            environment: BTreeMap::new(),
            timeout: Duration::from_secs(1),
            maximum_output_bytes: 4,
        })
        .expect("first output");
    assert_eq!(first.stdout, b"1234");

    let rejected = runner
        .run(&ToolRequest {
            executable: "/usr/bin/true".to_owned(),
            arguments: Vec::new(),
            environment: BTreeMap::new(),
            timeout: Duration::from_secs(1),
            maximum_output_bytes: 4,
        })
        .expect_err("retained output must consume aggregate budget");
    assert_eq!(rejected.code(), Code::ResourceExhausted);
    drop(first);
    let after_release = runner.run(&ToolRequest {
        executable: "/usr/bin/true".to_owned(),
        arguments: Vec::new(),
        environment: BTreeMap::new(),
        timeout: Duration::from_secs(1),
        maximum_output_bytes: 4,
    });
    assert!(after_release.is_ok(), "after release: {after_release:?}");
}

#[cfg(unix)]
#[test]
fn tool_timeout_terminates_grandchildren_in_the_process_group() {
    use mindclade_node_agent::{ProcessSupervisor, ToolRequest, ToolRunner};
    use std::collections::BTreeMap;
    use std::sync::Arc;
    use std::time::{Duration, SystemTime, UNIX_EPOCH};

    let pid_file = std::env::temp_dir().join(format!(
        "mindclade-tool-grandchild-{}-{}",
        std::process::id(),
        SystemTime::now()
            .duration_since(UNIX_EPOCH)
            .expect("clock")
            .as_nanos(),
    ));
    let supervisor = Arc::new(
        ProcessSupervisor::with_termination_grace(2, Duration::from_millis(100))
            .expect("supervisor"),
    );
    let runner = ToolRunner::new(supervisor.clone(), Duration::from_millis(1)).expect("runner");
    let output = runner
        .run(&ToolRequest {
            executable: "/bin/sh".to_owned(),
            arguments: vec![
                "-c".to_owned(),
                "/bin/sleep 60 & echo $! > \"$1\"; wait".to_owned(),
                "mindclade-tool".to_owned(),
                pid_file.to_string_lossy().into_owned(),
            ],
            environment: BTreeMap::new(),
            timeout: Duration::from_millis(100),
            maximum_output_bytes: 1024,
        })
        .expect("timeout should be supervised");
    assert!(output.timed_out);
    let grandchild = std::fs::read_to_string(&pid_file)
        .expect("grandchild pid")
        .trim()
        .to_owned();
    let alive = std::process::Command::new("/bin/kill")
        .args(["-0", &grandchild])
        .stdout(std::process::Stdio::null())
        .stderr(std::process::Stdio::null())
        .status()
        .expect("process existence check")
        .success();
    assert!(!alive, "grandchild {grandchild} survived tool timeout");
    assert_eq!(supervisor.active(), 0);
    let _ = std::fs::remove_file(pid_file);
}

#[test]
fn stage_context_is_cooperatively_cancellable_and_bounded() {
    use mindclade_node_agent::StageContext;
    use mindclade_runtime_core::{Budget, CancellationToken};

    let budget = Budget::root(
        "stage",
        ResourceVector::new().set(ResourceKind::ResidentMemoryBytes, 1024),
    );
    let cancellation = CancellationToken::new();
    let clock = mindclade_runtime_core::ManualClock::new(
        std::time::SystemTime::UNIX_EPOCH + std::time::Duration::from_millis(99),
        std::time::Instant::now(),
    );
    let context = StageContext::new(
        cancellation.clone(),
        100,
        budget,
        std::sync::Arc::new(clock.clone()),
    );
    assert!(context.ensure_active().is_ok());
    clock.advance(std::time::Duration::from_millis(2));
    assert!(context.ensure_active().is_err());
    cancellation.cancel("test");
    assert!(context.ensure_active().is_err());
}

#[test]
fn stage_accounting_underflow_fails_closed() {
    let health = mindclade_node_agent::NodeHealth::new();
    assert!(!health.end());
    let snapshot = health.snapshot();
    assert!(!snapshot.accounting_healthy);
    assert!(!snapshot.accepting);
    assert!(!health.begin());
}
