// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

//! Bounded protobuf framing between runtime-host and a local model worker.

use mindclade_faults::{Code, Fault, FaultResult};
use mindclade_process_os::current_user_id;
use mindclade_protocols::runtime::v1::{WorkerCommand, WorkerStatus};
use prost::Message;
use std::io::ErrorKind;
use std::os::unix::fs::{FileTypeExt, MetadataExt, PermissionsExt};
use std::path::Path;
use std::time::Duration;
use tokio::io::{AsyncReadExt, AsyncWriteExt, ReadHalf, WriteHalf};
use tokio::net::UnixStream;
use tokio::time::{Instant, sleep, timeout};

const MAX_FRAME_BYTES: usize = 1024 * 1024;
const MAX_SOCKET_PATH_BYTES: usize = 100;
const CONNECT_TIMEOUT: Duration = Duration::from_secs(5);
const IO_TIMEOUT: Duration = Duration::from_secs(30);

pub struct ModelWorkerConnection {
    reader: ReadHalf<UnixStream>,
    writer: WriteHalf<UnixStream>,
}

impl core::fmt::Debug for ModelWorkerConnection {
    fn fmt(&self, formatter: &mut core::fmt::Formatter<'_>) -> core::fmt::Result {
        formatter
            .debug_struct("ModelWorkerConnection")
            .finish_non_exhaustive()
    }
}

impl ModelWorkerConnection {
    pub async fn connect(path: &Path) -> FaultResult<Self> {
        validate_socket_path(path)?;
        let deadline = Instant::now().checked_add(CONNECT_TIMEOUT).ok_or_else(|| {
            Fault::new(Code::OutOfRange, "model-worker connect deadline overflow")
        })?;
        loop {
            match validate_existing_socket(path) {
                Ok(true) => {}
                Ok(false) => {
                    if Instant::now() >= deadline {
                        return Err(Fault::new(
                            Code::DeadlineExceeded,
                            "model-worker socket did not become ready",
                        ));
                    }
                    sleep(Duration::from_millis(10)).await;
                    continue;
                }
                Err(error) => return Err(error),
            }
            match UnixStream::connect(path).await {
                Ok(stream) => {
                    let (reader, writer) = tokio::io::split(stream);
                    return Ok(Self { reader, writer });
                }
                Err(error)
                    if matches!(
                        error.kind(),
                        ErrorKind::NotFound
                            | ErrorKind::ConnectionRefused
                            | ErrorKind::ConnectionReset
                    ) && Instant::now() < deadline =>
                {
                    sleep(Duration::from_millis(10)).await;
                }
                Err(error) => {
                    return Err(Fault::new(
                        Code::Unavailable,
                        "model-worker socket is unavailable",
                    )
                    .with_source(error));
                }
            }
        }
    }

    pub async fn send(&mut self, command: &WorkerCommand) -> FaultResult<()> {
        let mut bytes = Vec::with_capacity(command.encoded_len());
        command.encode(&mut bytes).map_err(|error| {
            Fault::new(Code::Internal, "model-worker command encoding failed").with_source(error)
        })?;
        write_frame(&mut self.writer, &bytes).await
    }

    pub async fn receive(&mut self) -> FaultResult<Option<WorkerStatus>> {
        let Some(bytes) = read_frame(&mut self.reader).await? else {
            return Ok(None);
        };
        WorkerStatus::decode(bytes.as_slice())
            .map(Some)
            .map_err(|error| {
                Fault::invalid_argument("model-worker status protobuf is invalid")
                    .with_source(error)
            })
    }
}

fn validate_socket_path(path: &Path) -> FaultResult<()> {
    if !path.is_absolute()
        || path.as_os_str().is_empty()
        || path.as_os_str().as_encoded_bytes().len() > MAX_SOCKET_PATH_BYTES
    {
        return Err(Fault::invalid_argument(
            "model-worker socket path is invalid",
        ));
    }
    Ok(())
}

fn validate_existing_socket(path: &Path) -> FaultResult<bool> {
    let metadata = match std::fs::symlink_metadata(path) {
        Ok(metadata) => metadata,
        Err(error) if error.kind() == ErrorKind::NotFound => return Ok(false),
        Err(error) => {
            return Err(
                Fault::new(Code::Unavailable, "model-worker socket inspection failed")
                    .with_source(error),
            );
        }
    };
    if !metadata.file_type().is_socket() {
        return Err(Fault::new(
            Code::FailedPrecondition,
            "model-worker endpoint is not a Unix socket",
        ));
    }
    if metadata.uid() != current_user_id() {
        return Err(Fault::new(
            Code::PermissionDenied,
            "model-worker socket owner does not match runtime-host",
        ));
    }
    if metadata.permissions().mode() & 0o077 != 0 {
        return Err(Fault::new(
            Code::PermissionDenied,
            "model-worker socket grants group or other access",
        ));
    }
    Ok(true)
}

async fn read_frame(reader: &mut ReadHalf<UnixStream>) -> FaultResult<Option<Vec<u8>>> {
    let length = match timeout(IO_TIMEOUT, reader.read_u32()).await {
        Err(_) => {
            return Err(Fault::new(
                Code::DeadlineExceeded,
                "model-worker frame header timed out",
            ));
        }
        Ok(Ok(length)) => usize::try_from(length)
            .map_err(|_| Fault::new(Code::OutOfRange, "model-worker frame length overflow"))?,
        Ok(Err(error)) if error.kind() == ErrorKind::UnexpectedEof => return Ok(None),
        Ok(Err(error)) => {
            return Err(
                Fault::new(Code::Unavailable, "model-worker frame header failed")
                    .with_source(error),
            );
        }
    };
    if length == 0 || length > MAX_FRAME_BYTES {
        return Err(Fault::new(
            Code::ResourceExhausted,
            "model-worker frame exceeds limit",
        ));
    }
    let mut bytes = vec![0_u8; length];
    timeout(IO_TIMEOUT, reader.read_exact(&mut bytes))
        .await
        .map_err(|_| Fault::new(Code::DeadlineExceeded, "model-worker frame body timed out"))?
        .map_err(|error| {
            Fault::new(Code::Unavailable, "model-worker frame body failed").with_source(error)
        })?;
    Ok(Some(bytes))
}

async fn write_frame(writer: &mut WriteHalf<UnixStream>, bytes: &[u8]) -> FaultResult<()> {
    if bytes.is_empty() || bytes.len() > MAX_FRAME_BYTES {
        return Err(Fault::new(
            Code::ResourceExhausted,
            "model-worker command frame exceeds limit",
        ));
    }
    let length = u32::try_from(bytes.len())
        .map_err(|_| Fault::new(Code::OutOfRange, "model-worker frame length exceeds u32"))?;
    timeout(IO_TIMEOUT, writer.write_u32(length))
        .await
        .map_err(|_| {
            Fault::new(
                Code::DeadlineExceeded,
                "model-worker frame header timed out",
            )
        })?
        .map_err(|error| {
            Fault::new(Code::Unavailable, "model-worker frame header failed").with_source(error)
        })?;
    timeout(IO_TIMEOUT, writer.write_all(bytes))
        .await
        .map_err(|_| Fault::new(Code::DeadlineExceeded, "model-worker frame body timed out"))?
        .map_err(|error| {
            Fault::new(Code::Unavailable, "model-worker frame body failed").with_source(error)
        })?;
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::{ModelWorkerConnection, read_frame, write_frame};
    use mindclade_protocols::runtime::v1::{
        HeartbeatCommand, WorkerCommand, WorkerState, WorkerStatus, worker_command,
    };
    use prost::Message;
    use tokio::io::AsyncWriteExt;

    #[tokio::test]
    async fn protobuf_frames_round_trip_over_a_bounded_socket() {
        let (client, server) = tokio::net::UnixStream::pair().expect("Unix pair should open");
        let (reader, writer) = tokio::io::split(client);
        let mut connection = ModelWorkerConnection { reader, writer };
        let (mut server_reader, mut server_writer) = tokio::io::split(server);

        let server = tokio::spawn(async move {
            let command = read_frame(&mut server_reader)
                .await
                .expect("command frame")
                .expect("command bytes");
            let command = WorkerCommand::decode(command.as_slice()).expect("command protobuf");
            assert_eq!(command.sequence, 7);
            let status = WorkerStatus {
                sequence: 11,
                ticket_id: "ticket_01890f2c7b7a70008000000000000001".to_owned(),
                fencing_token: 9,
                state: WorkerState::Running as i32,
                observed_unix_millis: 100,
                message: "running".to_owned(),
                outputs: Vec::new(),
                diagnostic_artifact_digest: String::new(),
            };
            let mut encoded = Vec::new();
            status.encode(&mut encoded).expect("status protobuf");
            write_frame(&mut server_writer, &encoded)
                .await
                .expect("status frame");
        });

        connection
            .send(&WorkerCommand {
                sequence: 7,
                command: Some(worker_command::Command::Heartbeat(HeartbeatCommand {
                    requested_at_unix_millis: 100,
                })),
            })
            .await
            .expect("send command");
        let status = connection
            .receive()
            .await
            .expect("receive status")
            .expect("status");
        assert_eq!(status.sequence, 11);
        server.await.expect("server task");
    }

    #[tokio::test]
    async fn oversized_frames_are_rejected_before_allocation() {
        let (client, mut server) = tokio::net::UnixStream::pair().expect("Unix pair should open");
        let (mut reader, _writer) = tokio::io::split(client);
        server
            .write_u32(u32::try_from(super::MAX_FRAME_BYTES + 1).expect("test length"))
            .await
            .expect("write length");
        assert!(read_frame(&mut reader).await.is_err());
    }
}
