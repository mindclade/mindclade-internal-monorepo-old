// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

//! Does the kernel actually stop anything?
//!
//! A sandbox nobody has watched block a syscall is a configuration file. These
//! tests install a real filter in a real child process and assert that the
//! kernel kills it for a denied call — and, just as importantly, that a control
//! child under the same filter completes its admitted work, so a passing denial
//! cannot be an artefact of the process dying for some unrelated reason.

/// Confinement is not exercised on this host, and says so out loud.
///
/// seccomp-BPF is a Linux kernel facility with no macOS or Windows equivalent
/// (the Darwin `sandbox_init` family is a different mechanism with a different
/// policy language, and is deprecated). There is nothing to install here and
/// therefore nothing to observe blocking. What *is* asserted on this host is the
/// fail-closed half: `tests/policy.rs` proves a required policy returns
/// `Unimplemented` rather than running unconfined.
#[cfg(not(target_os = "linux"))]
#[test]
fn kernel_enforcement_is_not_exercised_on_this_host() {
    eprintln!(
        "SKIPPED: seccomp-BPF enforcement requires Linux; this host is {}. \
         The fail-closed path is covered by tests/policy.rs::\
         a_required_policy_on_a_host_without_seccomp_refuses.",
        std::env::consts::OS
    );
}

#[cfg(target_os = "linux")]
mod linux {
    use mindclade_faults::Code;
    use mindclade_sandbox_os::{SandboxPolicy, SandboxRequest, Scope, Syscall, profiles};
    use std::io::Read;
    use std::os::unix::process::ExitStatusExt;
    use std::process::{Command, ExitStatus, Stdio};
    use std::time::{Duration, Instant};

    /// Selects probe behaviour when this test binary is re-executed as a child.
    const CHILD_MODE: &str = "MINDCLADE_SANDBOX_OS_PROBE";
    /// Path of the one test the child is asked to run.
    const CHILD_TEST: &str = "linux::confined_probe";
    /// `SECCOMP_RET_KILL_PROCESS` terminates the process as though by `SIGSYS`.
    const SIGSYS: i32 = 31;
    /// Bounded, like every other wait in this repository. A probe that has not
    /// finished by now is hung, and the test says so instead of hanging with it.
    const PROBE_DEADLINE: Duration = Duration::from_secs(30);
    const PROBE_POLL: Duration = Duration::from_millis(10);

    /// The probe body. Inert unless this process was launched as a child.
    ///
    /// Re-executing the test binary is what gives each filter its own process:
    /// a seccomp filter can never be removed, so installing one in the shared
    /// harness process would confine every test that ran afterwards.
    #[test]
    fn confined_probe() {
        let Ok(mode) = std::env::var(CHILD_MODE) else {
            return;
        };
        match mode.as_str() {
            "denied" => probe_denied(),
            "allowed" => probe_allowed(),
            "networked" => probe_networked(),
            other => panic!("unknown probe mode {other}"),
        }
    }

    fn probe_denied() {
        let policy = profiles::untrusted_input_worker()
            .expect("worker profile")
            .rescoped(Scope::CallingThread);
        install(policy);
        report("confined");
        // `socket` is not in the untrusted-input worker profile, so this is the
        // syscall the kernel is expected to refuse. If the filter is not in
        // force the bind succeeds and the probe reports its own escape.
        let bound = std::net::UdpSocket::bind("127.0.0.1:0");
        report(&format!("escaped: bind returned {}", bound.is_ok()));
        std::process::exit(2);
    }

    fn probe_allowed() {
        // Created before the filter exists; read back after, so the assertion is
        // about the filter admitting file IO rather than about file creation.
        let path = std::env::temp_dir().join(format!("sandbox_os_probe_{}", std::process::id()));
        std::fs::write(&path, b"admitted").expect("stage the probe file");
        let policy = profiles::untrusted_input_worker()
            .expect("worker profile")
            .rescoped(Scope::CallingThread);
        install(policy);
        report("confined");
        let contents = std::fs::read(&path).expect("read the staged file under the filter");
        assert_eq!(contents, b"admitted");
        report("admitted-work-completed");
        std::process::exit(0);
    }

    fn probe_networked() {
        // The production default: every thread, via TSYNC. Exits immediately
        // afterwards so the harness never tears down under the filter.
        let policy = profiles::networked_worker().expect("networked profile");
        assert_eq!(policy.scope(), Scope::Process);
        install(policy);
        report("confined");
        let socket = std::net::UdpSocket::bind("127.0.0.1:0").expect("bind under the network set");
        drop(socket);
        report("admitted-work-completed");
        std::process::exit(0);
    }

    fn install(policy: SandboxPolicy) {
        let confinement = mindclade_sandbox_os::install(&SandboxRequest::Required(policy))
            .expect("install the seccomp-BPF filter");
        assert!(confinement.is_enforced());
    }

    fn report(message: &str) {
        // A line the parent can assert on, flushed immediately: the next
        // statement may be the one the kernel kills the process for.
        use std::io::Write;
        let mut out = std::io::stdout();
        let _ = writeln!(out, "{message}");
        let _ = out.flush();
    }

    fn run_probe(mode: &str) -> (ExitStatus, String) {
        let binary = std::env::current_exe().expect("locate this test binary");
        let mut child = Command::new(binary)
            .args([CHILD_TEST, "--exact", "--nocapture"])
            .env(CHILD_MODE, mode)
            .stdin(Stdio::null())
            .stdout(Stdio::piped())
            .stderr(Stdio::null())
            .spawn()
            .expect("spawn the confined probe");
        let deadline = Instant::now()
            .checked_add(PROBE_DEADLINE)
            .expect("probe deadline");
        let status = loop {
            if let Some(status) = child.try_wait().expect("inspect the probe") {
                break status;
            }
            assert!(
                Instant::now() < deadline,
                "confined probe {mode} did not terminate within {PROBE_DEADLINE:?}"
            );
            std::thread::sleep(PROBE_POLL);
        };
        let mut output = String::new();
        child
            .stdout
            .take()
            .expect("probe stdout")
            .read_to_string(&mut output)
            .expect("read probe output");
        (status, output)
    }

    #[test]
    fn a_denied_syscall_kills_the_confined_process() {
        let (status, output) = run_probe("denied");
        assert!(
            output.contains("confined"),
            "probe died before it reached the denied call; output: {output}"
        );
        assert!(
            !output.contains("escaped"),
            "the filter did not stop `socket`; output: {output}"
        );
        assert_eq!(
            status.signal(),
            Some(SIGSYS),
            "expected the kernel to kill the probe with SIGSYS; status {status:?}, output: {output}"
        );
    }

    #[test]
    fn admitted_work_still_completes_under_the_filter() {
        // The control. Without it, the denial test above would also pass if the
        // filter killed the process for any reason at all.
        let (status, output) = run_probe("allowed");
        assert!(
            output.contains("admitted-work-completed"),
            "admitted file IO was blocked; output: {output}"
        );
        assert_eq!(status.code(), Some(0), "probe output: {output}");
    }

    #[test]
    fn the_networked_profile_installs_across_every_thread() {
        let (status, output) = run_probe("networked");
        assert!(
            output.contains("admitted-work-completed"),
            "the network set did not admit `socket`; output: {output}"
        );
        assert_eq!(status.code(), Some(0), "probe output: {output}");
    }

    #[test]
    fn a_policy_that_cannot_be_compiled_refuses_instead_of_installing() {
        // Fail closed on the other unavailability path: the policy is
        // well-formed as far as the builder can tell, but names a system call
        // this platform has no number for. A filter compiled with that rule
        // silently dropped would be weaker than the one the operator asked for,
        // so installation refuses rather than approximating it.
        let policy = SandboxPolicy::builder()
            .allow(mindclade_sandbox_os::MANDATORY_SYSCALLS)
            .expect("mandatory syscalls")
            .allow(&[Syscall::new("no_such_system_call")])
            .expect("within the allowlist bound")
            .build()
            .expect("the builder cannot know platform numbers");
        let fault = mindclade_sandbox_os::install(&SandboxRequest::Required(policy))
            .expect_err("an unresolvable syscall must not install a partial filter");
        assert_eq!(fault.code(), Code::InvalidArgument);
        assert!(
            fault.message().contains("platform table"),
            "fault must name the reason: {fault}"
        );
    }
}
