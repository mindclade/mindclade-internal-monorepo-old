// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

#![cfg(unix)]

use mindclade_process_os::{configure_process_group, terminate_process_group};
use std::io::{BufRead, BufReader};
use std::process::{Command, Stdio};
use std::time::Duration;

#[test]
fn termination_reaps_the_child_and_its_process_group() {
    let mut command = Command::new("sh");
    command
        .arg("-c")
        .arg("sleep 30 & printf '%s\\n' \"$!\"; wait")
        .stdin(Stdio::null())
        .stdout(Stdio::piped())
        .stderr(Stdio::null());
    configure_process_group(&mut command).expect("configure an isolated process group");

    let mut child = command.spawn().expect("spawn supervised process group");
    let stdout = child.stdout.take().expect("capture descendant identifier");
    let mut line = String::new();
    BufReader::new(stdout)
        .read_line(&mut line)
        .expect("read descendant identifier");
    let descendant = line.trim();
    assert!(
        !descendant.is_empty(),
        "shell did not report its descendant"
    );

    terminate_process_group(&mut child, Duration::from_millis(100))
        .expect("terminate and reap the complete process group");
    assert!(
        child.try_wait().expect("inspect reaped child").is_some(),
        "process-group leader was not reaped"
    );

    let status = Command::new("sh")
        .args(["-c", "kill -0 \"$1\" 2>/dev/null", "sh", descendant])
        .status()
        .expect("probe descendant process");
    assert!(
        !status.success(),
        "descendant survived process-group shutdown"
    );
}
