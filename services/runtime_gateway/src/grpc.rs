// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

//! Public server-streaming execution lifecycle and host cancellation bridge.

use crate::network::{GatewayNetworkState, round_admission_bytes, unix_millis};
use crate::protocol;
use hyper_util::rt::TokioIo;
use mindclade_faults::{Code, Fault, FaultResult, status};
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
    // Shared with the readiness probe rather than restated here: if what the
    // probe connects to could drift from what dispatch connects to, readiness
    // would stop describing this call.
    let path = crate::host_probe::host_socket_path(endpoint)
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
        tonic::Code::NotFound => Status::not_found("runtime-host has no such resource"),
        tonic::Code::AlreadyExists => {
            Status::already_exists("runtime-host is already running this execution")
        }
        tonic::Code::Aborted => Status::aborted("runtime-host aborted execution"),
        tonic::Code::OutOfRange => Status::out_of_range("runtime-host rejected execution"),
        tonic::Code::Unimplemented => {
            Status::unimplemented("runtime-host does not implement this operation")
        }
        tonic::Code::DataLoss => Status::data_loss("runtime-host lost execution state"),
        // What is sanitized here is the *message*, never the code. Every arm
        // replaces the peer's text with a fixed string, so no runtime-host
        // internal crosses the boundary either way — and a code carries no
        // internals to leak, only the class of failure.
        //
        // Six codes were previously collapsed into `unavailable` alongside
        // these four. That undid the fix on the only path a client sees: with
        // `services/runtime_host` now answering `not_found` for a model it does
        // not hold, collapsing it here would have handed the client
        // `unavailable` — retry, and page — for a request that can never
        // succeed. The four that remain are the ones where the host itself is
        // the failure, which is exactly what `unavailable` means to this
        // gateway's caller: a dependency is down, and retrying is correct.
        tonic::Code::Ok
        | tonic::Code::Unknown
        | tonic::Code::Internal
        | tonic::Code::Unavailable => Status::unavailable("runtime-host execution failed"),
    }
}

/// Renders one of this gateway's own faults as a gRPC status.
///
/// The table is `mindclade_faults::status`, which mirrors `libs/go/grpcx`. The
/// local `match` this replaces collapsed `NotFound`, `Aborted`, `Unimplemented`,
/// `DataLoss`, and `Unknown` into `internal` even though gRPC defines an exact
/// code for each, and rendered `OutOfRange` as `invalid_argument` and `Conflict`
/// as `already_exists`.
///
/// `tonic::Code::from(i32)` is total, and the canonical table never yields 0,
/// so a fault can never be rendered as `Ok`.
fn fault_status(error: &Fault) -> Status {
    Status::new(
        tonic::Code::from(status::grpc_code(error.code())),
        error.message(),
    )
}

#[cfg(test)]
mod fault_status_tests {
    use super::{Code, Fault, Status, fault_status, sanitize_host_status, status};

    /// Every fault code renders the canonical gRPC status.
    ///
    /// The codes are restated here rather than read back from
    /// `mindclade_faults::status`. Reading them back would assert only that
    /// this edge calls the shared function, and the defect being closed is
    /// several edges that each called nothing shared: these values are the
    /// client-visible contract and a change to any one of them has to break a
    /// test that names it.
    #[test]
    fn every_fault_code_renders_its_canonical_grpc_status() {
        let expected: &[(Code, tonic::Code)] = &[
            (Code::Cancelled, tonic::Code::Cancelled),
            (Code::Unknown, tonic::Code::Unknown),
            (Code::InvalidArgument, tonic::Code::InvalidArgument),
            (Code::DeadlineExceeded, tonic::Code::DeadlineExceeded),
            (Code::NotFound, tonic::Code::NotFound),
            (Code::AlreadyExists, tonic::Code::AlreadyExists),
            (Code::PermissionDenied, tonic::Code::PermissionDenied),
            (Code::ResourceExhausted, tonic::Code::ResourceExhausted),
            (Code::FailedPrecondition, tonic::Code::FailedPrecondition),
            (Code::Aborted, tonic::Code::Aborted),
            (Code::Conflict, tonic::Code::Aborted),
            (Code::OutOfRange, tonic::Code::OutOfRange),
            (Code::Unimplemented, tonic::Code::Unimplemented),
            (Code::Internal, tonic::Code::Internal),
            (Code::Unavailable, tonic::Code::Unavailable),
            (Code::DataLoss, tonic::Code::DataLoss),
            (Code::Unauthenticated, tonic::Code::Unauthenticated),
        ];
        assert_eq!(
            expected.len(),
            status::ALL.len(),
            "a fault code is missing from this table"
        );
        for &(code, want) in expected {
            let rendered = fault_status(&Fault::new(code, "rendered")).code();
            assert_eq!(rendered, want, "{code} rendered gRPC {rendered:?}");
        }
    }

    /// The regression. `NotFound`, `Aborted`, `Unimplemented`, `DataLoss`, and
    /// `Unknown` all collapsed into `internal` even though gRPC defines an
    /// exact code for each — so a caller could not tell "that does not exist"
    /// or "this method will never exist" from "we broke, retry".
    #[test]
    fn codes_with_an_exact_grpc_counterpart_do_not_collapse_into_internal() {
        for code in [
            Code::NotFound,
            Code::Aborted,
            Code::Unimplemented,
            Code::DataLoss,
            Code::Unknown,
        ] {
            let rendered = fault_status(&Fault::new(code, "rendered")).code();
            assert_ne!(
                rendered,
                tonic::Code::Internal,
                "{code} still collapses into internal"
            );
        }
    }

    /// A fault is a failure by construction, so it must never be rendered as a
    /// success status a client would read as a completed call.
    #[test]
    fn a_fault_is_never_rendered_as_ok() {
        for &code in status::ALL {
            assert_ne!(
                fault_status(&Fault::new(code, "rendered")).code(),
                tonic::Code::Ok,
                "{code} rendered as a success"
            );
        }
    }

    /// The relay path. Rendering the host's own faults correctly is worth
    /// nothing if the gateway flattens them again on the way out.
    #[test]
    fn relaying_a_host_status_preserves_what_the_caller_can_act_on() {
        let preserved = [
            tonic::Code::InvalidArgument,
            tonic::Code::Unauthenticated,
            tonic::Code::PermissionDenied,
            tonic::Code::ResourceExhausted,
            tonic::Code::FailedPrecondition,
            tonic::Code::DeadlineExceeded,
            tonic::Code::Cancelled,
            tonic::Code::NotFound,
            tonic::Code::AlreadyExists,
            tonic::Code::Aborted,
            tonic::Code::OutOfRange,
            tonic::Code::Unimplemented,
            tonic::Code::DataLoss,
        ];
        for code in preserved {
            let relayed = sanitize_host_status(&Status::new(code, "host detail"));
            assert_eq!(relayed.code(), code, "{code:?} was flattened");
        }
        for code in [
            tonic::Code::Unknown,
            tonic::Code::Internal,
            tonic::Code::Unavailable,
        ] {
            let relayed = sanitize_host_status(&Status::new(code, "host detail"));
            assert_eq!(relayed.code(), tonic::Code::Unavailable);
        }
    }

    /// The sanitizing half, which the arms above must not have weakened: the
    /// peer's message never crosses the trust boundary, whatever its code.
    #[test]
    fn relaying_a_host_status_never_forwards_its_message() {
        for &code in status::ALL {
            let host = Status::new(
                tonic::Code::from(status::grpc_code(code)),
                "worker /var/run/secret leaked this",
            );
            let relayed = sanitize_host_status(&host);
            assert!(
                !relayed.message().contains("secret"),
                "{code} forwarded the runtime-host message"
            );
        }
    }
}
