// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

//! Tokio Unix-socket control edge for local model-worker supervision.

use mindclade_faults::{Code, Fault, FaultResult};
use std::future::Future;
use std::io::ErrorKind;
use std::os::unix::fs::{FileTypeExt, MetadataExt, PermissionsExt};
use std::os::unix::net::UnixStream as StdUnixStream;
use std::path::{Path, PathBuf};
use std::pin::Pin;
use std::sync::Arc;
use std::time::Duration;
use tokio::io::{AsyncReadExt, AsyncWriteExt};
use tokio::net::{UnixListener, UnixStream};
use tokio::sync::watch;
use tokio::task::JoinSet;
use tokio::time::{Instant, timeout};

const MAX_FRAME_BYTES: usize = 1024 * 1024;
const MAX_CONTROL_CONNECTIONS: usize = 128;
const CONTROL_IO_TIMEOUT: Duration = Duration::from_secs(30);
const CONTROL_HANDLER_TIMEOUT: Duration = Duration::from_mins(5);
const CONNECTION_DRAIN_TIMEOUT: Duration = Duration::from_secs(30);

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
struct SocketIdentity {
    device: u64,
    inode: u64,
}

pub trait AsyncControlHandler: Send + Sync + 'static {
    fn handle<'a>(
        &'a self,
        request: Vec<u8>,
    ) -> Pin<Box<dyn Future<Output = FaultResult<Vec<u8>>> + Send + 'a>>;
}

/// Per-connection control session. `WorkerControl` is a streaming protocol, so
/// cancel/drain/heartbeat commands must be scoped to the execution that was
/// started on the same connection rather than a global process singleton.
pub trait AsyncControlSession: Send + 'static {
    fn handle<'a>(
        &'a mut self,
        request: Vec<u8>,
    ) -> Pin<Box<dyn Future<Output = FaultResult<Vec<u8>>> + Send + 'a>>;
}

/// Factory that creates isolated state for each accepted control connection.
pub trait AsyncControlSessionFactory: Send + Sync + 'static {
    fn open(&self) -> FaultResult<Box<dyn AsyncControlSession>>;
}

pub async fn serve_unix(
    path: PathBuf,
    handler: Arc<dyn AsyncControlHandler>,
    mut shutdown: watch::Receiver<bool>,
) -> FaultResult<()> {
    let (listener, socket_identity) = bind_secure_listener(&path)?;
    let mut connections = JoinSet::new();

    loop {
        tokio::select! {
            changed = shutdown.changed() => {
                if changed.is_err() || *shutdown.borrow() {
                    break;
                }
            }
            accepted = listener.accept(), if connections.len() < MAX_CONTROL_CONNECTIONS => {
                let (stream, _) = accepted.map_err(|error| {
                    Fault::new(Code::Unavailable, "runtime-host control accept failed")
                        .with_source(error)
                })?;
                let handler = Arc::clone(&handler);
                connections.spawn(async move { handle_connection(stream, handler).await });
            }
            completed = connections.join_next(), if !connections.is_empty() => {
                if let Some(result) = completed {
                    match result {
                        Ok(Ok(())) => {}
                        Ok(Err(_fault)) => {
                            // Connection faults are isolated to the bounded client session.
                            // Service telemetry owns reporting at the composition edge.
                        }
                        Err(join_error) if join_error.is_cancelled() => {}
                        Err(join_error) => {
                            return Err(Fault::new(
                                Code::Internal,
                                "runtime-host control task failed",
                            )
                            .with_source(join_error));
                        }
                    }
                }
            }
        }
    }

    drain_connections(&mut connections).await?;
    remove_owned_socket(&path, socket_identity)
}

pub async fn serve_unix_sessions(
    path: PathBuf,
    factory: Arc<dyn AsyncControlSessionFactory>,
    mut shutdown: watch::Receiver<bool>,
) -> FaultResult<()> {
    let (listener, socket_identity) = bind_secure_listener(&path)?;
    let mut connections = JoinSet::new();

    loop {
        tokio::select! {
            changed = shutdown.changed() => {
                if changed.is_err() || *shutdown.borrow() {
                    break;
                }
            }
            accepted = listener.accept(), if connections.len() < MAX_CONTROL_CONNECTIONS => {
                let (stream, _) = accepted.map_err(|error| {
                    Fault::new(Code::Unavailable, "runtime-host control accept failed")
                        .with_source(error)
                })?;
                let factory = Arc::clone(&factory);
                connections.spawn(async move {
                    let mut session = factory.open()?;
                    handle_session_connection(stream, session.as_mut()).await
                });
            }
            completed = connections.join_next(), if !connections.is_empty() => {
                if let Some(result) = completed {
                    match result {
                        Ok(Ok(())) => {}
                        Ok(Err(_fault)) => {}
                        Err(join_error) if join_error.is_cancelled() => {}
                        Err(join_error) => {
                            return Err(Fault::new(
                                Code::Internal,
                                "runtime-host control task failed",
                            )
                            .with_source(join_error));
                        }
                    }
                }
            }
        }
    }

    drain_connections(&mut connections).await?;
    remove_owned_socket(&path, socket_identity)
}

async fn handle_session_connection(
    mut stream: UnixStream,
    session: &mut dyn AsyncControlSession,
) -> FaultResult<()> {
    loop {
        let length = match timeout(CONTROL_IO_TIMEOUT, stream.read_u32()).await {
            Err(_) => {
                return Err(Fault::new(
                    Code::DeadlineExceeded,
                    "control frame header read timed out",
                ));
            }
            Ok(Ok(length)) => usize::try_from(length)
                .map_err(|_| Fault::new(Code::OutOfRange, "control frame length exceeds usize"))?,
            Ok(Err(error)) if error.kind() == ErrorKind::UnexpectedEof => return Ok(()),
            Ok(Err(error)) => {
                return Err(
                    Fault::new(Code::Unavailable, "control frame header read failed")
                        .with_source(error),
                );
            }
        };
        if length == 0 || length > MAX_FRAME_BYTES {
            return Err(Fault::new(
                Code::ResourceExhausted,
                "control frame exceeds runtime-host limit",
            ));
        }
        let mut request = vec![0_u8; length];
        timeout(CONTROL_IO_TIMEOUT, stream.read_exact(&mut request))
            .await
            .map_err(|_| Fault::new(Code::DeadlineExceeded, "control frame body read timed out"))?
            .map_err(|error| {
                Fault::new(Code::Unavailable, "control frame body read failed").with_source(error)
            })?;
        let response = timeout(CONTROL_HANDLER_TIMEOUT, session.handle(request))
            .await
            .map_err(|_| Fault::new(Code::DeadlineExceeded, "control handler timed out"))??;
        write_response(&mut stream, response).await?;
    }
}

async fn write_response(stream: &mut UnixStream, response: Vec<u8>) -> FaultResult<()> {
    if response.len() > MAX_FRAME_BYTES {
        return Err(Fault::new(
            Code::ResourceExhausted,
            "control response exceeds runtime-host limit",
        ));
    }
    let response_len = u32::try_from(response.len())
        .map_err(|_| Fault::new(Code::OutOfRange, "control response length exceeds u32"))?;
    timeout(CONTROL_IO_TIMEOUT, stream.write_u32(response_len))
        .await
        .map_err(|_| Fault::new(Code::DeadlineExceeded, "control response write timed out"))?
        .map_err(|error| {
            Fault::new(Code::Unavailable, "control response header write failed").with_source(error)
        })?;
    timeout(CONTROL_IO_TIMEOUT, stream.write_all(&response))
        .await
        .map_err(|_| Fault::new(Code::DeadlineExceeded, "control response write timed out"))?
        .map_err(|error| {
            Fault::new(Code::Unavailable, "control response body write failed").with_source(error)
        })?;
    Ok(())
}

async fn drain_connections(connections: &mut JoinSet<FaultResult<()>>) -> FaultResult<()> {
    let deadline = Instant::now() + CONNECTION_DRAIN_TIMEOUT;
    while !connections.is_empty() {
        let remaining = deadline
            .checked_duration_since(Instant::now())
            .unwrap_or(Duration::ZERO);
        if remaining.is_zero() {
            connections.abort_all();
            while connections.join_next().await.is_some() {}
            return Err(Fault::new(
                Code::DeadlineExceeded,
                "runtime-host control connections exceeded drain deadline",
            ));
        }
        match timeout(remaining, connections.join_next()).await {
            Ok(Some(Ok(Ok(())))) => {}
            Ok(Some(Ok(Err(_fault)))) => {}
            Ok(Some(Err(join_error))) if join_error.is_cancelled() => {}
            Ok(Some(Err(join_error))) => {
                return Err(
                    Fault::new(Code::Internal, "runtime-host control task failed")
                        .with_source(join_error),
                );
            }
            Ok(None) => break,
            Err(_) => {
                connections.abort_all();
                while connections.join_next().await.is_some() {}
                return Err(Fault::new(
                    Code::DeadlineExceeded,
                    "runtime-host control connections exceeded drain deadline",
                ));
            }
        }
    }
    Ok(())
}

async fn handle_connection(
    mut stream: UnixStream,
    handler: Arc<dyn AsyncControlHandler>,
) -> FaultResult<()> {
    loop {
        let length = match timeout(CONTROL_IO_TIMEOUT, stream.read_u32()).await {
            Err(_) => {
                return Err(Fault::new(
                    Code::DeadlineExceeded,
                    "control frame header read timed out",
                ));
            }
            Ok(Ok(length)) => usize::try_from(length)
                .map_err(|_| Fault::new(Code::OutOfRange, "control frame length exceeds usize"))?,
            Ok(Err(error)) if error.kind() == ErrorKind::UnexpectedEof => return Ok(()),
            Ok(Err(error)) => {
                return Err(
                    Fault::new(Code::Unavailable, "control frame header read failed")
                        .with_source(error),
                );
            }
        };
        if length == 0 || length > MAX_FRAME_BYTES {
            return Err(Fault::new(
                Code::ResourceExhausted,
                "control frame exceeds runtime-host limit",
            ));
        }
        let mut request = vec![0_u8; length];
        timeout(CONTROL_IO_TIMEOUT, stream.read_exact(&mut request))
            .await
            .map_err(|_| Fault::new(Code::DeadlineExceeded, "control frame body read timed out"))?
            .map_err(|error| {
                Fault::new(Code::Unavailable, "control frame body read failed").with_source(error)
            })?;
        let response = timeout(CONTROL_HANDLER_TIMEOUT, handler.handle(request))
            .await
            .map_err(|_| Fault::new(Code::DeadlineExceeded, "control handler timed out"))??;
        write_response(&mut stream, response).await?;
    }
}

fn prepare_socket_path(path: &Path) -> FaultResult<()> {
    if path.as_os_str().is_empty() || path.as_os_str().as_encoded_bytes().len() > 100 {
        return Err(Fault::invalid_argument(
            "runtime-host Unix socket path is invalid",
        ));
    }
    if !path.is_absolute() {
        return Err(Fault::invalid_argument(
            "runtime-host Unix socket path must be absolute",
        ));
    }
    if let Some(parent) = path.parent() {
        std::fs::create_dir_all(parent).map_err(|error| {
            Fault::new(
                Code::Unavailable,
                "failed to create runtime-host socket directory",
            )
            .with_source(error)
        })?;
    }
    let metadata = match std::fs::symlink_metadata(path) {
        Ok(metadata) => metadata,
        Err(error) if error.kind() == ErrorKind::NotFound => return Ok(()),
        Err(error) => {
            return Err(Fault::new(
                Code::Unavailable,
                "failed to inspect runtime-host socket path",
            )
            .with_source(error));
        }
    };
    if !metadata.file_type().is_socket() {
        return Err(Fault::new(
            Code::AlreadyExists,
            "runtime-host socket path exists and is not a socket",
        ));
    }
    match StdUnixStream::connect(path) {
        Ok(_) => Err(Fault::new(
            Code::AlreadyExists,
            "runtime-host control socket is already active",
        )),
        Err(error) if error.kind() == ErrorKind::ConnectionRefused => std::fs::remove_file(path)
            .map_err(|error| {
                Fault::new(
                    Code::Unavailable,
                    "failed to remove stale runtime-host socket",
                )
                .with_source(error)
            }),
        Err(error) => Err(Fault::new(
            Code::Unavailable,
            "failed to determine whether runtime-host socket is active",
        )
        .with_source(error)),
    }
}

fn bind_secure_listener(path: &Path) -> FaultResult<(UnixListener, SocketIdentity)> {
    prepare_socket_path(path)?;
    let listener = UnixListener::bind(path).map_err(|error| {
        Fault::new(
            Code::Unavailable,
            "failed to bind runtime-host control socket",
        )
        .with_source(error)
    })?;
    std::fs::set_permissions(path, std::fs::Permissions::from_mode(0o600)).map_err(|error| {
        let _ = std::fs::remove_file(path);
        Fault::new(
            Code::Unavailable,
            "failed to secure runtime-host control socket",
        )
        .with_source(error)
    })?;
    let metadata = std::fs::symlink_metadata(path).map_err(|error| {
        let _ = std::fs::remove_file(path);
        Fault::new(
            Code::Unavailable,
            "failed to inspect bound runtime-host control socket",
        )
        .with_source(error)
    })?;
    Ok((
        listener,
        SocketIdentity {
            device: metadata.dev(),
            inode: metadata.ino(),
        },
    ))
}

fn remove_owned_socket(path: &Path, expected: SocketIdentity) -> FaultResult<()> {
    let metadata = match std::fs::symlink_metadata(path) {
        Ok(metadata) => metadata,
        Err(error) if error.kind() == ErrorKind::NotFound => return Ok(()),
        Err(error) => {
            return Err(Fault::new(
                Code::Unavailable,
                "failed to inspect runtime-host control socket during shutdown",
            )
            .with_source(error));
        }
    };
    let actual = SocketIdentity {
        device: metadata.dev(),
        inode: metadata.ino(),
    };
    if !metadata.file_type().is_socket() || actual != expected {
        return Err(Fault::new(
            Code::FailedPrecondition,
            "runtime-host socket path ownership changed during shutdown",
        ));
    }
    std::fs::remove_file(path).map_err(|error| {
        Fault::new(
            Code::Unavailable,
            "failed to remove runtime-host control socket during shutdown",
        )
        .with_source(error)
    })
}

#[cfg(test)]
mod tests {
    use super::prepare_socket_path;
    use std::fs;
    #[cfg(target_os = "linux")]
    use std::os::unix::net::UnixListener;
    use std::time::{SystemTime, UNIX_EPOCH};

    fn temporary_path(name: &str) -> std::path::PathBuf {
        std::path::PathBuf::from("/tmp").join(format!(
            "mc-rh-{name}-{}-{}.sock",
            std::process::id(),
            SystemTime::now()
                .duration_since(UNIX_EPOCH)
                .expect("clock should be after epoch")
                .as_nanos()
        ))
    }

    #[test]
    fn socket_preparation_never_removes_a_regular_file() {
        let path = temporary_path("regular");
        fs::write(&path, b"preserve").expect("fixture should be created");
        assert!(prepare_socket_path(&path).is_err());
        assert_eq!(fs::read(&path).expect("fixture should remain"), b"preserve");
        fs::remove_file(path).expect("fixture should be removed");
    }

    // The macOS workspace sandbox prohibits creating Unix-domain listeners;
    // Linux CI exercises the active-socket probe used in production.
    #[cfg(target_os = "linux")]
    #[test]
    fn socket_preparation_rejects_an_active_listener() {
        let path = temporary_path("active");
        let listener = UnixListener::bind(&path).expect("fixture listener should bind");
        assert!(prepare_socket_path(&path).is_err());
        assert!(path.exists());
        drop(listener);
        fs::remove_file(path).expect("fixture socket should be removed");
    }
}
