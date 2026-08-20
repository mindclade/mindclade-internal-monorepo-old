// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

//! Public server-streaming execution lifecycle and host cancellation bridge.

use crate::network::{GatewayNetworkState, round_admission_bytes, unix_millis};
use crate::protocol;
use hyper_util::rt::TokioIo;
use mindclade_faults::{Code, Fault, FaultResult};
use mindclade_protocols::runtime::v1::runtime_execution_server::{
    RuntimeExecution, RuntimeExecutionServer,
};
use mindclade_protocols::runtime::v1::worker_control_client::WorkerControlClient;
use mindclade_protocols::runtime::v1::{
    CancelCommand, RuntimeExecuteRequest, StartCommand, WorkerCommand, WorkerState, WorkerStatus,
    worker_command,
};
use mindclade_runtime_core::{BytePermit, FencingToken};
use mindclade_serving_runtime::AdmissionPermit;
use prost::Message;
use std::net::SocketAddr;
use std::path::PathBuf;
use std::pin::Pin;
use std::task::{Context, Poll};
use std::time::Duration;
use tokio::net::{TcpListener, UnixStream};
use tokio::sync::{mpsc, watch};
use tokio_stream::Stream;
use tokio_stream::wrappers::{ReceiverStream, TcpListenerStream};
use tonic::transport::{Channel, Endpoint, Server};
use tonic::{Request, Response, Status};
use tower::service_fn;

const MAX_CONTROL_BYTES: usize = 1024 * 1024;
const MAX_INPUT_DESCRIPTORS: usize = 1024;
const STATUS_QUEUE_CAPACITY: usize = 32;
const COMMAND_QUEUE_CAPACITY: usize = 4;
const CONNECT_TIMEOUT: Duration = Duration::from_secs(5);
const CANCELLATION_GRACE: Duration = Duration::from_secs(5);
const MAX_EXECUTION_DURATION: Duration = Duration::from_mins(5);
const GRPC_DRAIN_TIMEOUT: Duration = Duration::from_secs(30);

#[derive(Clone, Debug)]
struct ExecutionService {
    state: GatewayNetworkState,
}

#[tonic::async_trait]
impl RuntimeExecution for ExecutionService {
    type ExecuteStream = Pin<Box<dyn Stream<Item = Result<WorkerStatus, Status>> + Send + 'static>>;

    async fn execute(
        &self,
        request: Request<RuntimeExecuteRequest>,
    ) -> Result<Response<Self::ExecuteStream>, Status> {
        if !self.state.execution_enabled() {
            return Err(Status::unavailable(
                "runtime execution endpoint is disabled",
            ));
        }
        let encoded_bytes = u64::try_from(request.get_ref().encoded_len())
            .map_err(|_| Status::resource_exhausted("execution request exceeds platform limits"))?;
        let request_memory = self
            .state
            .reserve_request_bytes(
                round_admission_bytes(encoded_bytes).map_err(|error| fault_status(&error))?,
            )
            .map_err(|error| fault_status(&error))?;
        let message = request.into_inner();
        validate_execute_request(&message).map_err(|error| fault_status(&error))?;

        let wire_ticket = message
            .ticket
            .clone()
            .ok_or_else(|| Status::invalid_argument("execution ticket is missing"))?;
        let ticket = protocol::execution_ticket(wire_ticket.clone())
            .map_err(|error| fault_status(&error))?;
        let inference = protocol::inference_request(
            message
                .dispatch
                .ok_or_else(|| Status::invalid_argument("dispatch request is missing"))?,
        )
        .map_err(|error| fault_status(&error))?;
        let now = unix_millis().map_err(|error| fault_status(&error))?;
        let admitted = self
            .state
            .core()
            .admit_execution(inference, &ticket, now)
            .map_err(|error| fault_status(&error))?;
        let endpoint = admitted.route.endpoint.clone();
        let admission_permit = admitted.permit;

        let mut client = connect_host(&endpoint).await?;
        let (command_sender, command_receiver) = mpsc::channel(COMMAND_QUEUE_CAPACITY);
        command_sender
            .send(WorkerCommand {
                sequence: 1,
                command: Some(worker_command::Command::Start(StartCommand {
                    ticket: Some(wire_ticket),
                    inputs: message.inputs,
                    operation: message.operation,
                })),
            })
            .await
            .map_err(|_| Status::unavailable("runtime-host command stream closed"))?;
        let response = client
            .execute(ReceiverStream::new(command_receiver))
            .await
            .map_err(|error| sanitize_host_status(&error))?;
        drop(request_memory);

        let (sender, receiver) = mpsc::channel(STATUS_QUEUE_CAPACITY);
        let expected_ticket = ticket.claims.ticket_id.to_string();
        let expected_fencing = ticket.claims.fencing_token;
        let deadline = execution_deadline(now, ticket.claims.deadline_unix_millis)?;
        let state = self.state.clone();
        tokio::spawn(relay_statuses(
            response.into_inner(),
            command_sender,
            sender,
            state,
            admission_permit,
            expected_ticket,
            expected_fencing,
            deadline,
        ));
        Ok(Response::new(Box::pin(ExecutionStatusStream { receiver })))
    }
}

pub async fn serve(
    address: SocketAddr,
    state: GatewayNetworkState,
    mut shutdown: watch::Receiver<bool>,
) -> FaultResult<()> {
    let listener = TcpListener::bind(address).await.map_err(|error| {
        Fault::new(Code::Unavailable, "runtime gateway gRPC listener failed").with_source(error)
    })?;
    let incoming = TcpListenerStream::new(listener);
    let mut server_shutdown = shutdown.clone();
    let server = Server::builder()
        .add_service(
            RuntimeExecutionServer::new(ExecutionService { state })
                .max_decoding_message_size(MAX_CONTROL_BYTES)
                .max_encoding_message_size(MAX_CONTROL_BYTES),
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
                return result.map_err(|error| {
                    Fault::new(Code::Unavailable, "runtime gateway gRPC server failed")
                        .with_source(error)
                });
            }
            changed = shutdown.changed() => {
                if changed.is_err() || *shutdown.borrow() {
                    return tokio::time::timeout(GRPC_DRAIN_TIMEOUT, &mut server)
                        .await
                        .map_err(|_| Fault::new(
                            Code::DeadlineExceeded,
                            "runtime gateway gRPC drain exceeded its deadline",
                        ))?
                        .map_err(|error| {
                            Fault::new(Code::Unavailable, "runtime gateway gRPC server failed")
                                .with_source(error)
                        });
                }
            }
        }
    }
}

enum RelayItem {
    Status(WorkerStatus, BytePermit),
    Error(Status),
}

struct ExecutionStatusStream {
    receiver: mpsc::Receiver<RelayItem>,
}

impl Stream for ExecutionStatusStream {
    type Item = Result<WorkerStatus, Status>;

    fn poll_next(mut self: Pin<&mut Self>, context: &mut Context<'_>) -> Poll<Option<Self::Item>> {
        match self.receiver.poll_recv(context) {
            Poll::Ready(Some(RelayItem::Status(status, _memory))) => Poll::Ready(Some(Ok(status))),
            Poll::Ready(Some(RelayItem::Error(error))) => Poll::Ready(Some(Err(error))),
            Poll::Ready(None) => Poll::Ready(None),
            Poll::Pending => Poll::Pending,
        }
    }
}

#[allow(clippy::too_many_arguments)]
async fn relay_statuses(
    mut host: tonic::Streaming<WorkerStatus>,
    command_sender: mpsc::Sender<WorkerCommand>,
    sender: mpsc::Sender<RelayItem>,
    state: GatewayNetworkState,
    _admission_permit: AdmissionPermit,
    expected_ticket: String,
    expected_fencing: FencingToken,
    deadline: tokio::time::Instant,
) {
    let mut last_sequence = 0_u64;
    loop {
        tokio::select! {
            () = sender.closed() => {
                cancel_host(&command_sender, &mut host, "client execution stream closed").await;
                break;
            }
            () = tokio::time::sleep_until(deadline) => {
                let _ = sender.send(RelayItem::Error(Status::deadline_exceeded(
                    "execution deadline exceeded",
                ))).await;
                cancel_host(&command_sender, &mut host, "execution deadline exceeded").await;
                break;
            }
            message = host.message() => {
                let status = match message {
                    Ok(Some(status)) => status,
                    Ok(None) => {
                        let _ = sender.send(RelayItem::Error(Status::unavailable(
                            "runtime-host status stream ended before a terminal state",
                        ))).await;
                        break;
                    }
                    Err(error) => {
                        let _ = sender.send(RelayItem::Error(sanitize_host_status(&error))).await;
                        break;
                    }
                };
                if status.sequence == 0
                    || status.sequence <= last_sequence
                    || status.ticket_id != expected_ticket
                    || status.fencing_token != expected_fencing.get()
                {
                    let _ = sender.send(RelayItem::Error(Status::data_loss(
                        "runtime-host returned an invalid status sequence or identity",
                    ))).await;
                    cancel_host(&command_sender, &mut host, "invalid host status").await;
                    break;
                }
                last_sequence = status.sequence;
                let terminal = terminal_state(status.state);
                let bytes = match u64::try_from(status.encoded_len())
                    .map_err(|_| Fault::new(Code::OutOfRange, "worker status size exceeds u64"))
                    .and_then(round_admission_bytes)
                    .and_then(|bytes| state.reserve_response_bytes(bytes))
                {
                    Ok(memory) => memory,
                    Err(error) => {
                        let _ = sender.send(RelayItem::Error(fault_status(&error))).await;
                        cancel_host(&command_sender, &mut host, "gateway response budget exhausted").await;
                        break;
                    }
                };
                if sender.send(RelayItem::Status(status, bytes)).await.is_err() {
                    cancel_host(&command_sender, &mut host, "client execution stream closed").await;
                    break;
                }
                if terminal {
                    break;
                }
            }
        }
    }
}

async fn cancel_host(
    command_sender: &mpsc::Sender<WorkerCommand>,
    host: &mut tonic::Streaming<WorkerStatus>,
    reason: &str,
) {
    let now = unix_millis().unwrap_or(0);
    let Ok(grace_millis) = u64::try_from(CANCELLATION_GRACE.as_millis()) else {
        return;
    };
    let Some(deadline) = now.checked_add(grace_millis) else {
        return;
    };
    if command_sender
        .send(WorkerCommand {
            sequence: 2,
            command: Some(worker_command::Command::Cancel(CancelCommand {
                reason: reason.to_owned(),
                deadline_unix_millis: deadline,
            })),
        })
        .await
        .is_err()
    {
        return;
    }
    let _ = tokio::time::timeout(CANCELLATION_GRACE, async {
        while let Ok(Some(status)) = host.message().await {
            if terminal_state(status.state) {
                break;
            }
        }
    })
    .await;
}

async fn connect_host(endpoint: &str) -> Result<WorkerControlClient<Channel>, Status> {
    let path = endpoint
        .strip_prefix("unix://")
        .map(PathBuf::from)
        .filter(|path| path.is_absolute() && path.as_os_str().as_encoded_bytes().len() <= 100)
        .ok_or_else(|| Status::failed_precondition("runtime-host route endpoint is invalid"))?;
    let connector_path = path.clone();
    let channel = Endpoint::from_static("http://[::]:50051")
        .connect_timeout(CONNECT_TIMEOUT)
        .timeout(MAX_EXECUTION_DURATION)
        .connect_with_connector(service_fn(move |_| {
            let path = connector_path.clone();
            async move { UnixStream::connect(path).await.map(TokioIo::new) }
        }))
        .await
        .map_err(|_| Status::unavailable("runtime-host gRPC endpoint is unavailable"))?;
    Ok(WorkerControlClient::new(channel)
        .max_decoding_message_size(MAX_CONTROL_BYTES)
        .max_encoding_message_size(MAX_CONTROL_BYTES))
}

fn validate_execute_request(message: &RuntimeExecuteRequest) -> FaultResult<()> {
    if message.operation.is_empty()
        || message.operation.len() > 256
        || message.operation.trim() != message.operation
        || message.inputs.len() > MAX_INPUT_DESCRIPTORS
    {
        return Err(Fault::invalid_argument(
            "runtime execution request is invalid",
        ));
    }
    Ok(())
}

fn execution_deadline(now: u64, signed_deadline: u64) -> Result<tokio::time::Instant, Status> {
    let remaining_millis = signed_deadline
        .checked_sub(now)
        .ok_or_else(|| Status::deadline_exceeded("execution ticket deadline has expired"))?;
    let remaining = Duration::from_millis(remaining_millis).min(MAX_EXECUTION_DURATION);
    tokio::time::Instant::now()
        .checked_add(remaining)
        .ok_or_else(|| Status::out_of_range("execution deadline exceeds clock range"))
}

fn terminal_state(state: i32) -> bool {
    matches!(
        WorkerState::try_from(state).ok(),
        Some(WorkerState::Completed | WorkerState::Cancelled | WorkerState::Failed)
    )
}

fn sanitize_host_status(status: &Status) -> Status {
    match status.code() {
        tonic::Code::InvalidArgument => Status::invalid_argument("runtime-host rejected execution"),
        tonic::Code::Unauthenticated => Status::unauthenticated("runtime-host rejected authority"),
        tonic::Code::PermissionDenied => {
            Status::permission_denied("runtime-host rejected authority")
        }
        tonic::Code::ResourceExhausted => {
            Status::resource_exhausted("runtime-host resources exhausted")
        }
        tonic::Code::FailedPrecondition => {
            Status::failed_precondition("runtime-host precondition failed")
        }
        tonic::Code::DeadlineExceeded => {
            Status::deadline_exceeded("runtime-host deadline exceeded")
        }
        tonic::Code::Cancelled => Status::cancelled("runtime-host execution cancelled"),
        _ => Status::unavailable("runtime-host execution failed"),
    }
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
        _ => Status::internal("runtime gateway execution failed"),
    }
}
