// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

//! Bounded response multiplexing independent of any network framework.

use mindclade_faults::{Code, Fault, FaultResult};
use mindclade_runtime_core::CancellationToken;
use std::sync::{
    Arc, Mutex,
    mpsc::{self, Receiver, RecvTimeoutError, SyncSender},
};
use std::time::Duration;

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct ResponseChunk {
    pub sequence: u64,
    pub payload: Vec<u8>,
    pub terminal: bool,
}

#[derive(Debug)]
struct SendState {
    next_sequence: u64,
    output_bytes: u64,
    terminal_sent: bool,
}

#[derive(Clone, Debug)]
pub struct StreamSender {
    tx: SyncSender<ResponseChunk>,
    state: Arc<Mutex<SendState>>,
    cancellation: CancellationToken,
    maximum_chunk_bytes: usize,
    maximum_output_bytes: u64,
}

#[derive(Debug)]
pub struct StreamReceiver {
    rx: Receiver<ResponseChunk>,
    cancellation: CancellationToken,
}

pub fn bounded_stream(
    capacity: usize,
    maximum_chunk_bytes: usize,
    maximum_output_bytes: u64,
) -> FaultResult<(StreamSender, StreamReceiver)> {
    if capacity == 0 || maximum_chunk_bytes == 0 || maximum_output_bytes == 0 {
        return Err(Fault::invalid_argument("stream limits must be positive"));
    }
    let (tx, rx) = mpsc::sync_channel(capacity);
    let cancellation = CancellationToken::new();
    Ok((
        StreamSender {
            tx,
            state: Arc::new(Mutex::new(SendState {
                next_sequence: 1,
                output_bytes: 0,
                terminal_sent: false,
            })),
            cancellation: cancellation.clone(),
            maximum_chunk_bytes,
            maximum_output_bytes,
        },
        StreamReceiver { rx, cancellation },
    ))
}

impl StreamSender {
    pub fn send(&self, payload: Vec<u8>) -> FaultResult<()> {
        self.send_inner(payload, false)
    }
    pub fn finish(&self, payload: Vec<u8>) -> FaultResult<()> {
        self.send_inner(payload, true)
    }
    pub fn cancel(&self, reason: impl Into<String>) {
        self.cancellation.cancel(reason);
    }
    fn send_inner(&self, payload: Vec<u8>, terminal: bool) -> FaultResult<()> {
        if self.cancellation.is_cancelled() {
            return Err(Fault::new(Code::Cancelled, "response stream is cancelled"));
        }
        if payload.len() > self.maximum_chunk_bytes {
            return Err(Fault::new(
                Code::ResourceExhausted,
                "response chunk exceeds its byte limit",
            ));
        }
        let mut state = self.state.lock().unwrap_or_else(|p| p.into_inner());
        if state.terminal_sent {
            return Err(Fault::new(
                Code::FailedPrecondition,
                "response stream is already terminal",
            ));
        }
        let payload_bytes = u64::try_from(payload.len())
            .map_err(|_| Fault::new(Code::OutOfRange, "response chunk length exceeds u64"))?;
        let next_bytes = state
            .output_bytes
            .checked_add(payload_bytes)
            .ok_or_else(|| Fault::new(Code::OutOfRange, "response stream accounting overflow"))?;
        if next_bytes > self.maximum_output_bytes {
            return Err(Fault::new(
                Code::ResourceExhausted,
                "response stream output budget exceeded",
            ));
        }
        let next_sequence = state
            .next_sequence
            .checked_add(1)
            .ok_or_else(|| Fault::new(Code::OutOfRange, "response stream sequence exhausted"))?;
        let chunk = ResponseChunk {
            sequence: state.next_sequence,
            payload,
            terminal,
        };
        self.tx.try_send(chunk).map_err(|error| match error {
            mpsc::TrySendError::Full(_) => Fault::new(
                Code::ResourceExhausted,
                "response stream backpressure limit reached",
            ),
            mpsc::TrySendError::Disconnected(_) => {
                Fault::new(Code::Cancelled, "response consumer disconnected")
            }
        })?;
        state.next_sequence = next_sequence;
        state.output_bytes = next_bytes;
        state.terminal_sent = terminal;
        Ok(())
    }
}

impl StreamReceiver {
    pub fn recv_timeout(&self, timeout: Duration) -> FaultResult<Option<ResponseChunk>> {
        if self.cancellation.is_cancelled() {
            return Err(Fault::new(Code::Cancelled, "response stream is cancelled"));
        }
        match self.rx.recv_timeout(timeout) {
            Ok(chunk) => Ok(Some(chunk)),
            Err(RecvTimeoutError::Timeout) => Ok(None),
            Err(RecvTimeoutError::Disconnected) => Ok(None),
        }
    }
    pub fn cancel(&self, reason: impl Into<String>) {
        self.cancellation.cancel(reason);
    }
}
