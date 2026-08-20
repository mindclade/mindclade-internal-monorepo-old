// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

//! Process-group creation and bounded Unix process-tree termination.
#![allow(clippy::missing_errors_doc)]

use mindclade_faults::{Code, Fault, FaultResult};
use std::process::{Child, Command};
use std::time::{Duration, Instant};

pub const DEFAULT_TERMINATION_GRACE: Duration = Duration::from_secs(5);
pub const MAXIMUM_TERMINATION_GRACE: Duration = Duration::from_secs(30);
const TERMINATION_POLL_INTERVAL: Duration = Duration::from_millis(10);
const KILL_REAP_TIMEOUT: Duration = Duration::from_secs(1);

/// Effective Unix user identity used to validate local IPC ownership.
#[cfg(unix)]
#[must_use]
pub fn current_user_id() -> u32 {
    // SAFETY: `geteuid` takes no arguments, returns a scalar value, and does
    // not borrow or mutate Rust-owned memory.
    unsafe { libc::geteuid() }
}

#[cfg(not(unix))]
#[must_use]
pub fn current_user_id() -> u32 {
    0
}

#[cfg(unix)]
pub fn configure_process_group(command: &mut Command) -> FaultResult<()> {
    use std::os::unix::process::CommandExt;
    command.process_group(0);
    Ok(())
}

#[cfg(not(unix))]
pub fn configure_process_group(_command: &mut Command) -> FaultResult<()> {
    Err(Fault::new(
        Code::Unimplemented,
        "process-tree supervision requires Unix process groups",
    ))
}

#[cfg(unix)]
pub fn terminate_process_group(child: &mut Child, grace: Duration) -> FaultResult<()> {
    if grace > MAXIMUM_TERMINATION_GRACE {
        return Err(Fault::invalid_argument(
            "process termination grace exceeds supported bound",
        ));
    }
    let process_group = i32::try_from(child.id()).map_err(|_| {
        Fault::new(
            Code::OutOfRange,
            "child process identifier exceeds Unix pid range",
        )
    })?;

    signal_group(process_group, libc::SIGTERM)?;
    let graceful_deadline = Instant::now()
        .checked_add(grace)
        .ok_or_else(|| Fault::new(Code::OutOfRange, "termination deadline overflow"))?;
    loop {
        let _ = child.try_wait().map_err(|error| {
            Fault::new(Code::Unavailable, "failed to inspect child process").with_source(error)
        })?;
        if !process_group_exists(process_group)? {
            child.wait().map_err(|error| {
                Fault::new(Code::Unavailable, "failed to reap child process").with_source(error)
            })?;
            return Ok(());
        }
        if Instant::now() >= graceful_deadline {
            break;
        }
        std::thread::sleep(TERMINATION_POLL_INTERVAL);
    }

    signal_group(process_group, libc::SIGKILL)?;
    child.wait().map_err(|error| {
        Fault::new(Code::Unavailable, "failed to reap killed child process").with_source(error)
    })?;

    let reap_deadline = Instant::now()
        .checked_add(KILL_REAP_TIMEOUT)
        .ok_or_else(|| Fault::new(Code::OutOfRange, "process reap deadline overflow"))?;
    while process_group_exists(process_group)? {
        if Instant::now() >= reap_deadline {
            return Err(Fault::new(
                Code::DeadlineExceeded,
                "process group remained after forced termination",
            ));
        }
        std::thread::sleep(TERMINATION_POLL_INTERVAL);
    }
    Ok(())
}

#[cfg(not(unix))]
pub fn terminate_process_group(_child: &mut Child, _grace: Duration) -> FaultResult<()> {
    Err(Fault::new(
        Code::Unimplemented,
        "process-tree supervision requires Unix process groups",
    ))
}

#[cfg(unix)]
fn signal_group(process_group: i32, signal: i32) -> FaultResult<()> {
    // SAFETY: `process_group` comes from a successfully spawned owned Child and
    // is range-checked above. A negative pid addresses that process group. No
    // pointer, borrowed memory, or Rust-owned allocation crosses the FFI call.
    let result = unsafe { libc::kill(-process_group, signal) };
    if result == 0 {
        return Ok(());
    }
    let error = std::io::Error::last_os_error();
    if error.raw_os_error() == Some(libc::ESRCH) {
        Ok(())
    } else {
        Err(Fault::new(Code::Unavailable, "failed to signal process group").with_source(error))
    }
}

#[cfg(unix)]
fn process_group_exists(process_group: i32) -> FaultResult<bool> {
    // SAFETY: signal zero performs existence/permission checking only. The
    // range-checked negative pid denotes the owned process group.
    let result = unsafe { libc::kill(-process_group, 0) };
    if result == 0 {
        return Ok(true);
    }
    let error = std::io::Error::last_os_error();
    match error.raw_os_error() {
        Some(libc::ESRCH) => Ok(false),
        Some(libc::EPERM) => Ok(true),
        _ => {
            Err(Fault::new(Code::Unavailable, "failed to inspect process group").with_source(error))
        }
    }
}
