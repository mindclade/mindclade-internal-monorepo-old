// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

//! Hermetic external-tool execution with bounded retained output, complete pipe
//! draining, explicit environment, and a hard wall-clock deadline.
use crate::ProcessSupervisor;
use mindclade_faults::{Code, Fault, FaultResult};
use std::collections::BTreeMap;
use std::io::Read;
use std::path::Path;
use std::process::{Command, Stdio};
use std::sync::Arc;
use std::thread::{self, JoinHandle};
use std::time::{Duration, Instant};

/// Hard per-stream retention ceiling for external tool output.
pub const MAXIMUM_TOOL_OUTPUT_BYTES: u64 = 16 * 1024 * 1024;
/// Hard wall-clock ceiling for one external tool invocation.
pub const MAXIMUM_TOOL_TIMEOUT: Duration = Duration::from_hours(1);
const MINIMUM_POLL_INTERVAL: Duration = Duration::from_millis(1);
const MAXIMUM_POLL_INTERVAL: Duration = Duration::from_secs(1);

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct ToolRequest {
    pub executable: String,
    pub arguments: Vec<String>,
    pub environment: BTreeMap<String, String>,
    pub timeout: Duration,
    pub maximum_output_bytes: u64,
}
impl ToolRequest {
    pub fn validate(&self) -> FaultResult<()> {
        if self.executable.is_empty()
            || self.executable.len() > 4096
            || self.arguments.len() > 512
            || self.environment.len() > 256
            || self.timeout.is_zero()
            || self.timeout > MAXIMUM_TOOL_TIMEOUT
            || self.maximum_output_bytes == 0
            || self.maximum_output_bytes > MAXIMUM_TOOL_OUTPUT_BYTES
            || !Path::new(&self.executable).is_absolute()
            || self.executable.contains('\0')
        {
            return Err(Fault::invalid_argument("tool request is invalid"));
        }
        if self
            .arguments
            .iter()
            .any(|value| value.len() > 16 * 1024 || value.contains('\0'))
            || self.environment.iter().any(|(key, value)| {
                key.is_empty()
                    || key.contains(['=', '\0'])
                    || key.len() > 128
                    || value.len() > 16 * 1024
                    || value.contains('\0')
            })
        {
            return Err(Fault::invalid_argument(
                "tool arguments/environment exceed limits",
            ));
        }
        Ok(())
    }
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct ToolOutput {
    pub exit_code: Option<i32>,
    pub stdout: Vec<u8>,
    pub stderr: Vec<u8>,
    pub stdout_truncated: bool,
    pub stderr_truncated: bool,
    pub timed_out: bool,
}

#[derive(Clone, Debug)]
pub struct ToolRunner {
    supervisor: Arc<ProcessSupervisor>,
    poll_interval: Duration,
}
impl ToolRunner {
    pub fn new(supervisor: Arc<ProcessSupervisor>, poll_interval: Duration) -> FaultResult<Self> {
        if !(MINIMUM_POLL_INTERVAL..=MAXIMUM_POLL_INTERVAL).contains(&poll_interval) {
            return Err(Fault::invalid_argument(
                "tool poll interval is outside supported bounds",
            ));
        }
        Ok(Self {
            supervisor,
            poll_interval,
        })
    }
    pub fn run(&self, request: &ToolRequest) -> FaultResult<ToolOutput> {
        request.validate()?;
        let mut command = Command::new(&request.executable);
        command
            .args(&request.arguments)
            .env_clear()
            .stdin(Stdio::null())
            .stdout(Stdio::piped())
            .stderr(Stdio::piped());
        for (key, value) in &request.environment {
            command.env(key, value);
        }
        let mut child = command.spawn().map_err(|error| {
            Fault::new(Code::Unavailable, "failed to start external tool").with_source(error)
        })?;
        let Some(stdout) = child.stdout.take() else {
            terminate_unmanaged(&mut child);
            return Err(Fault::internal("tool stdout pipe was not created"));
        };
        let Some(stderr) = child.stderr.take() else {
            terminate_unmanaged(&mut child);
            return Err(Fault::internal("tool stderr pipe was not created"));
        };
        let stdout_reader =
            match spawn_pipe_reader("tool-stdout", stdout, request.maximum_output_bytes) {
                Ok(reader) => reader,
                Err(fault) => {
                    terminate_unmanaged(&mut child);
                    return Err(fault);
                }
            };
        let stderr_reader =
            match spawn_pipe_reader("tool-stderr", stderr, request.maximum_output_bytes) {
                Ok(reader) => reader,
                Err(fault) => {
                    terminate_unmanaged(&mut child);
                    let _ = join_pipe_reader(stdout_reader, "stdout");
                    return Err(fault);
                }
            };
        let process = self.supervisor.register(child)?;
        let deadline = Instant::now()
            .checked_add(request.timeout)
            .ok_or_else(|| Fault::new(Code::OutOfRange, "tool deadline exceeds clock range"))?;
        let (status, timed_out) = loop {
            let polled = {
                let mut children = self
                    .supervisor
                    .children
                    .lock()
                    .unwrap_or_else(std::sync::PoisonError::into_inner);
                let child = children
                    .get_mut(&process.pid)
                    .ok_or_else(|| Fault::internal("supervised child disappeared"))?;
                child.try_wait()
            };
            let status = match polled {
                Ok(status) => status,
                Err(error) => {
                    let _ = self.supervisor.terminate(process);
                    let _ = join_pipe_reader(stdout_reader, "stdout");
                    let _ = join_pipe_reader(stderr_reader, "stderr");
                    return Err(
                        Fault::new(Code::Unavailable, "failed to poll external tool")
                            .with_source(error),
                    );
                }
            };
            if let Some(status) = status {
                break (Some(status), false);
            }
            let now = Instant::now();
            if now >= deadline {
                self.supervisor.terminate(process)?;
                break (None, true);
            }
            thread::sleep(self.poll_interval.min(deadline.duration_since(now)));
        };
        // If the process exited normally, remove its already-reaped handle from
        // the registry. On timeout, terminate() already removed it.
        if !timed_out {
            self.supervisor
                .children
                .lock()
                .unwrap_or_else(std::sync::PoisonError::into_inner)
                .remove(&process.pid);
        }
        let stdout = join_pipe_reader(stdout_reader, "stdout")?;
        let stderr = join_pipe_reader(stderr_reader, "stderr")?;
        Ok(ToolOutput {
            exit_code: status.and_then(|value| value.code()),
            stdout: stdout.bytes,
            stderr: stderr.bytes,
            stdout_truncated: stdout.truncated,
            stderr_truncated: stderr.truncated,
            timed_out,
        })
    }
}

fn terminate_unmanaged(child: &mut std::process::Child) {
    let _ = child.kill();
    let _ = child.wait();
}

#[derive(Debug)]
struct CapturedPipe {
    bytes: Vec<u8>,
    truncated: bool,
}

fn spawn_pipe_reader<R: Read + Send + 'static>(
    name: &str,
    reader: R,
    maximum: u64,
) -> FaultResult<JoinHandle<FaultResult<CapturedPipe>>> {
    thread::Builder::new()
        .name(name.to_owned())
        .spawn(move || drain_pipe(reader, maximum))
        .map_err(|error| Fault::internal("failed to spawn tool pipe reader").with_source(error))
}
fn join_pipe_reader(
    handle: JoinHandle<FaultResult<CapturedPipe>>,
    stream: &str,
) -> FaultResult<CapturedPipe> {
    handle.join().map_err(|_| {
        Fault::internal("tool pipe reader panicked").with_context("stream", stream.to_owned())
    })?
}
fn drain_pipe<R: Read>(mut reader: R, maximum: u64) -> FaultResult<CapturedPipe> {
    let capacity = usize::try_from(maximum.min(16 * 1024 * 1024)).map_err(|_| {
        Fault::new(
            Code::OutOfRange,
            "tool output retention capacity exceeds usize",
        )
    })?;
    let mut retained = Vec::with_capacity(capacity);
    let mut truncated = false;
    let mut chunk = [0_u8; 16 * 1024];
    loop {
        let read = reader.read(&mut chunk).map_err(|error| {
            Fault::new(Code::Unavailable, "failed to read tool output").with_source(error)
        })?;
        if read == 0 {
            break;
        }
        let retained_bytes = u64::try_from(retained.len())
            .map_err(|_| Fault::new(Code::OutOfRange, "retained tool output length exceeds u64"))?;
        let remaining = maximum
            .checked_sub(retained_bytes)
            .ok_or_else(|| Fault::data_loss("tool output retention accounting underflow"))?;
        let read_bytes = u64::try_from(read)
            .map_err(|_| Fault::new(Code::OutOfRange, "tool output read length exceeds u64"))?;
        let keep = if remaining >= read_bytes {
            read
        } else {
            usize::try_from(remaining).map_err(|_| {
                Fault::new(
                    Code::OutOfRange,
                    "tool output retention remainder exceeds usize",
                )
            })?
        };
        retained.extend_from_slice(&chunk[..keep]);
        if keep < read {
            truncated = true;
        }
    }
    Ok(CapturedPipe {
        bytes: retained,
        truncated,
    })
}
