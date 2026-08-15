// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

//! Tokio Unix-socket control edge for local model-worker supervision.

use mindclade_faults::{Code, Fault, FaultResult};
use std::future::Future;
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
const CONNECTION_DRAIN_TIMEOUT: Duration = Duration::from_secs(30);

pub trait AsyncControlHandler: Send + Sync + 'static {
    fn handle<'a>(
        &'a self,
        request: Vec<u8>,
    ) -> Pin<Box<dyn Future<Output = FaultResult<Vec<u8>>> + Send + 'a>>;
}

/// Per-connection control session. WorkerControl is a streaming protocol, so
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
    prepare_socket_path(&path)?;
    let listener = UnixListener::bind(&path).map_err(|error| {
        Fault::new(
            Code::Unavailable,
            "failed to bind runtime-host control socket",
        )
        .with_source(error)
    })?;
    let mut connections = JoinSet::new();

    loop {
        tokio::select! {
            changed = shutdown.changed() => {
                if changed.is_err() || *shutdown.borrow() {
                    break;
                }
            }
            accepted = listener.accept() => {
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
    match std::fs::remove_file(&path) {
        Ok(()) => Ok(()),
        Err(error) if error.kind() == std::io::ErrorKind::NotFound => Ok(()),
        Err(error) => Err(Fault::new(
            Code::Unavailable,
            "failed to remove runtime-host control socket during shutdown",
        )
        .with_source(error)),
    }
}

pub async fn serve_unix_sessions(
    path: PathBuf,
    factory: Arc<dyn AsyncControlSessionFactory>,
    mut shutdown: watch::Receiver<bool>,
) -> FaultResult<()> {
    prepare_socket_path(&path)?;
    let listener = UnixListener::bind(&path).map_err(|error| {
        Fault::new(
            Code::Unavailable,
            "failed to bind runtime-host control socket",
        )
        .with_source(error)
    })?;
    let mut connections = JoinSet::new();

    loop {
        tokio::select! {
            changed = shutdown.changed() => {
                if changed.is_err() || *shutdown.borrow() {
                    break;
                }
            }
            accepted = listener.accept() => {
                let (stream, _) = accepted.map_err(|error| {
                    Fault::new(Code::Unavailable, "runtime-host control accept failed")
                        .with_source(error)
                })?;
                let mut session = factory.open()?;
                connections.spawn(async move { handle_session_connection(stream, session.as_mut()).await });
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
    match std::fs::remove_file(&path) {
        Ok(()) => Ok(()),
        Err(error) if error.kind() == std::io::ErrorKind::NotFound => Ok(()),
        Err(error) => Err(Fault::new(
            Code::Unavailable,
            "failed to remove runtime-host control socket during shutdown",
        )
        .with_source(error)),
    }
}

async fn handle_session_connection(
    mut stream: UnixStream,
    session: &mut dyn AsyncControlSession,
) -> FaultResult<()> {
    loop {
        let length = match stream.read_u32().await {
            Ok(length) => usize::try_from(length)
                .map_err(|_| Fault::new(Code::OutOfRange, "control frame length exceeds usize"))?,
            Err(error) if error.kind() == std::io::ErrorKind::UnexpectedEof => return Ok(()),
            Err(error) => {
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
        stream.read_exact(&mut request).await.map_err(|error| {
            Fault::new(Code::Unavailable, "control frame body read failed").with_source(error)
        })?;
        let response = session.handle(request).await?;
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
    stream.write_u32(response_len).await.map_err(|error| {
        Fault::new(Code::Unavailable, "control response header write failed").with_source(error)
    })?;
    stream.write_all(&response).await.map_err(|error| {
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
        let length = match stream.read_u32().await {
            Ok(length) => usize::try_from(length)
                .map_err(|_| Fault::new(Code::OutOfRange, "control frame length exceeds usize"))?,
            Err(error) if error.kind() == std::io::ErrorKind::UnexpectedEof => return Ok(()),
            Err(error) => {
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
        stream.read_exact(&mut request).await.map_err(|error| {
            Fault::new(Code::Unavailable, "control frame body read failed").with_source(error)
        })?;
        let response = handler.handle(request).await?;
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
    match std::fs::remove_file(path) {
        Ok(()) => Ok(()),
        Err(error) if error.kind() == std::io::ErrorKind::NotFound => Ok(()),
        Err(error) => Err(Fault::new(
            Code::Unavailable,
            "failed to remove stale runtime-host socket",
        )
        .with_source(error)),
    }
}
