// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

//! Tonic worker-control service over a permission-restricted Unix socket.

use crate::async_ipc::{bind_secure_listener, remove_owned_socket};
use crate::bootstrap::ControlSession;
use crate::{HostAuthority, HostCore};
use mindclade_faults::{Code, Fault, FaultResult};
use mindclade_protocols::runtime::v1::worker_control_server::{WorkerControl, WorkerControlServer};
use mindclade_protocols::runtime::v1::{WorkerCommand, WorkerStatus};
use std::path::PathBuf;
use std::pin::Pin;
use std::sync::Arc;
use std::time::Duration;
use tokio::sync::{mpsc, watch};
use tokio_stream::Stream;
use tokio_stream::wrappers::{ReceiverStream, UnixListenerStream};
use tonic::{Request, Response, Status};

const MAX_CONTROL_FRAME_BYTES: usize = 1024 * 1024;
const STATUS_QUEUE_CAPACITY: usize = 32;
const GRPC_DRAIN_TIMEOUT: Duration = Duration::from_secs(30);

#[derive(Clone, Debug)]
pub struct WorkerControlService {
    host: Arc<HostCore>,
    authority: Arc<HostAuthority>,
}

impl WorkerControlService {
    #[must_use]
    pub fn new(host: Arc<HostCore>, authority: Arc<HostAuthority>) -> Self {
        Self { host, authority }
    }
}

#[tonic::async_trait]
impl WorkerControl for WorkerControlService {
    type ExecuteStream = Pin<Box<dyn Stream<Item = Result<WorkerStatus, Status>> + Send + 'static>>;

    async fn execute(
        &self,
        request: Request<tonic::Streaming<WorkerCommand>>,
    ) -> Result<Response<Self::ExecuteStream>, Status> {
        let mut inbound = request.into_inner();
        let mut session = ControlSession::new(self.host.clone(), self.authority.clone());
        let (sender, receiver) = mpsc::channel(STATUS_QUEUE_CAPACITY);
        tokio::spawn(async move {
            loop {
                let command = match inbound.message().await {
                    Ok(Some(command)) => command,
                    Ok(None) => break,
                    Err(error) => {
                        let _ = sender.send(Err(error)).await;
                        break;
                    }
                };
                match session.handle_command(command) {
                    Ok(status) => {
                        if sender.send(Ok(status)).await.is_err() {
                            break;
                        }
                    }
                    Err(error) => {
                        let _ = sender.send(Err(fault_status(&error))).await;
                        break;
                    }
                }
            }
        });
        Ok(Response::new(Box::pin(ReceiverStream::new(receiver))))
    }
}

pub async fn serve_unix(
    path: PathBuf,
    service: WorkerControlService,
    mut shutdown: watch::Receiver<bool>,
) -> FaultResult<()> {
    let (listener, identity) = bind_secure_listener(&path)?;
    let incoming = UnixListenerStream::new(listener);
    let mut server_shutdown = shutdown.clone();
    let server = tonic::transport::Server::builder()
        .add_service(
            WorkerControlServer::new(service)
                .max_decoding_message_size(MAX_CONTROL_FRAME_BYTES)
                .max_encoding_message_size(MAX_CONTROL_FRAME_BYTES),
        )
        .serve_with_incoming_shutdown(incoming, async move {
            loop {
                if *server_shutdown.borrow() || server_shutdown.changed().await.is_err() {
                    break;
                }
            }
        });
    tokio::pin!(server);

    loop {
        tokio::select! {
            result = &mut server => {
                result.map_err(|error| {
                    Fault::new(Code::Unavailable, "runtime-host gRPC server failed")
                        .with_source(error)
                })?;
                break;
            }
            changed = shutdown.changed() => {
                if changed.is_err() || *shutdown.borrow() {
                    tokio::time::timeout(GRPC_DRAIN_TIMEOUT, &mut server)
                        .await
                        .map_err(|_| Fault::new(
                            Code::DeadlineExceeded,
                            "runtime-host gRPC drain exceeded its deadline",
                        ))?
                        .map_err(|error| {
                            Fault::new(Code::Unavailable, "runtime-host gRPC server failed")
                                .with_source(error)
                        })?;
                    break;
                }
            }
        }
    }
    remove_owned_socket(&path, identity)
}

fn fault_status(error: &Fault) -> Status {
    match error.code() {
        Code::InvalidArgument | Code::OutOfRange => Status::invalid_argument(error.message()),
        Code::Unauthenticated => Status::unauthenticated(error.message()),
        Code::PermissionDenied => Status::permission_denied(error.message()),
        Code::AlreadyExists | Code::Conflict => Status::already_exists(error.message()),
        Code::ResourceExhausted => Status::resource_exhausted(error.message()),
        Code::FailedPrecondition => Status::failed_precondition(error.message()),
        Code::DeadlineExceeded => Status::deadline_exceeded(error.message()),
        Code::Cancelled => Status::cancelled(error.message()),
        Code::Unavailable => Status::unavailable(error.message()),
        _ => Status::internal("runtime-host worker control failed"),
    }
}
