// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

//! Durable segmented telemetry spool.
#![forbid(unsafe_code)]
pub mod batch;
pub mod delivery;
pub mod envelope;
pub mod journal;
pub mod spool;

// delivery.rs imports this as `crate::DeliveryBatch`, alongside TelemetrySpool which is
// defined in this file. Re-exported rather than rewriting the import, because every other
// public type in this crate (Envelope, SpoolConfig, TelemetrySpool) is already named from the
// root and batch::DeliveryBatch is the odd one out.
pub use batch::DeliveryBatch;
use mindclade_atomic_fs::{AtomicFileStore, RelativePath};
use mindclade_bytes_io::ByteSize;
use mindclade_faults::{Code, Fault, FaultResult};
use mindclade_identifiers::ResourceId;
use mindclade_record_io::{Decoder, Encoder, FRAME_HEADER_BYTES, RecordReader, RecordWriter};
use mindclade_runtime_core::Clock;
use std::fs::{self, OpenOptions};
use std::io::{BufReader, BufWriter};
use std::path::{Path, PathBuf};
use std::sync::{Arc, Mutex};
use std::time::UNIX_EPOCH;

pub const ENVELOPE_SCHEMA: u16 = 1;

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct Envelope {
    pub event_id: ResourceId,
    pub sequence: u64,
    pub timestamp_millis: u64,
    pub event_type: String,
    pub payload: Vec<u8>,
}

impl Envelope {
    pub fn encode(&self) -> FaultResult<Vec<u8>> {
        let mut e = Encoder::new();
        e.u16(ENVELOPE_SCHEMA);
        e.string(&self.event_id.to_string())?;
        e.u64(self.sequence);
        e.u64(self.timestamp_millis);
        e.string(&self.event_type)?;
        e.bytes(&self.payload)?;
        Ok(e.into_bytes())
    }
    pub fn decode(bytes: &[u8], maximum_payload: usize) -> FaultResult<Self> {
        let message_limit = maximum_payload
            .checked_add(4096)
            .ok_or_else(|| Fault::new(Code::OutOfRange, "telemetry envelope limit overflow"))?;
        let mut d = Decoder::new(bytes, message_limit)?;
        if d.u16()? != ENVELOPE_SCHEMA {
            return Err(Fault::new(
                Code::FailedPrecondition,
                "telemetry envelope schema is unsupported",
            ));
        }
        let event_id = d.string()?.parse::<ResourceId>().map_err(|error| {
            Fault::data_loss("telemetry event ID is invalid").with_source(error)
        })?;
        let sequence = d.u64()?;
        let timestamp_millis = d.u64()?;
        let event_type = d.string()?.to_owned();
        let payload = d.bytes()?.to_vec();
        d.finish()?;
        if event_id.kind() != "event"
            || sequence == 0
            || event_type.is_empty()
            || event_type.len() > 256
            || !event_type.is_ascii()
            || payload.len() > maximum_payload
        {
            return Err(Fault::invalid_argument(
                "telemetry envelope bounds are invalid",
            ));
        }
        Ok(Self {
            event_id,
            sequence,
            timestamp_millis,
            event_type,
            payload,
        })
    }
}

#[derive(Clone, Debug)]
pub struct SpoolConfig {
    pub maximum_event_bytes: ByteSize,
    pub maximum_segment_bytes: ByteSize,
    pub maximum_total_bytes: ByteSize,
}

impl Default for SpoolConfig {
    fn default() -> Self {
        Self {
            maximum_event_bytes: ByteSize::new(4 * 1024 * 1024),
            maximum_segment_bytes: ByteSize::new(64 * 1024 * 1024),
            maximum_total_bytes: ByteSize::new(4 * 1024 * 1024 * 1024),
        }
    }
}

#[derive(Debug)]
struct State {
    next_sequence: u64,
    segment: u64,
    acknowledged: u64,
}

pub struct TelemetrySpool {
    root: PathBuf,
    files: AtomicFileStore,
    config: SpoolConfig,
    clock: Arc<dyn Clock>,
    state: Mutex<State>,
}

impl TelemetrySpool {
    pub fn open(
        root: impl Into<PathBuf>,
        config: SpoolConfig,
        clock: Arc<dyn Clock>,
    ) -> FaultResult<Self> {
        let root = root.into();
        let files = AtomicFileStore::new(&root)?;
        if config.maximum_event_bytes.get() == 0
            || config.maximum_segment_bytes.get() <= config.maximum_event_bytes.get()
            || config.maximum_total_bytes.get() < config.maximum_segment_bytes.get()
        {
            return Err(Fault::invalid_argument(
                "telemetry spool size limits are invalid",
            ));
        }
        let next_sequence = read_counter(&root.join("sequence"))?.unwrap_or(1);
        let acknowledged = read_counter(&root.join("ack"))?.unwrap_or(0);
        let segment = discover_segment(&root)?;
        Ok(Self {
            root,
            files,
            config,
            clock,
            state: Mutex::new(State {
                next_sequence,
                segment,
                acknowledged,
            }),
        })
    }
    pub fn append(&self, event_type: impl Into<String>, payload: &[u8]) -> FaultResult<Envelope> {
        let payload_bytes = u64::try_from(payload.len())
            .map_err(|_| Fault::new(Code::OutOfRange, "telemetry event length exceeds u64"))?;
        if payload_bytes > self.config.maximum_event_bytes.get() {
            return Err(Fault::new(
                Code::ResourceExhausted,
                "telemetry event exceeds limit",
            ));
        }
        let mut state = self
            .state
            .lock()
            .unwrap_or_else(std::sync::PoisonError::into_inner);
        let event_type = event_type.into();
        if event_type.is_empty() || event_type.len() > 256 || !event_type.is_ascii() {
            return Err(Fault::invalid_argument("telemetry event type is invalid"));
        }
        let event_id = ResourceId::generate("event", self.clock.as_ref()).map_err(|error| {
            Fault::internal("failed to generate telemetry event ID").with_source(error)
        })?;
        let timestamp = self
            .clock
            .system_now()
            .duration_since(UNIX_EPOCH)
            .map_err(|error| {
                Fault::new(
                    Code::FailedPrecondition,
                    "telemetry clock is before Unix epoch",
                )
                .with_source(error)
            })?;
        let timestamp_millis = u64::try_from(timestamp.as_millis()).map_err(|_| {
            Fault::new(
                Code::OutOfRange,
                "telemetry timestamp exceeds u64 milliseconds",
            )
        })?;
        let envelope = Envelope {
            event_id,
            sequence: state.next_sequence,
            timestamp_millis,
            event_type,
            payload: payload.to_vec(),
        };
        let encoded = envelope.encode()?;
        let encoded_bytes = u64::try_from(encoded.len())
            .map_err(|_| Fault::new(Code::OutOfRange, "telemetry envelope encoding exceeds u64"))?;
        let frame_overhead = u64::try_from(FRAME_HEADER_BYTES)
            .map_err(|_| Fault::new(Code::OutOfRange, "record frame header exceeds u64"))?;
        let append_bytes = encoded_bytes
            .checked_add(frame_overhead)
            .ok_or_else(|| Fault::new(Code::OutOfRange, "telemetry frame length overflow"))?;
        let mut path = segment_path(&self.root, state.segment);
        let current_size = match fs::metadata(&path) {
            Ok(meta) => meta.len(),
            Err(error) if error.kind() == std::io::ErrorKind::NotFound => 0,
            Err(error) => {
                return Err(
                    Fault::internal("failed to inspect telemetry segment").with_source(error)
                );
            }
        };
        let projected = current_size
            .checked_add(append_bytes)
            .ok_or_else(|| Fault::new(Code::OutOfRange, "telemetry segment size overflow"))?;
        if projected > self.config.maximum_segment_bytes.get() {
            state.segment = state
                .segment
                .checked_add(1)
                .ok_or_else(|| Fault::new(Code::OutOfRange, "telemetry segment number overflow"))?;
            path = segment_path(&self.root, state.segment);
        }
        enforce_total_budget(
            &self.root,
            self.config.maximum_total_bytes.get(),
            append_bytes,
        )?;
        let file = OpenOptions::new()
            .create(true)
            .append(true)
            .open(&path)
            .map_err(|error| {
                Fault::internal("failed to open telemetry segment").with_source(error)
            })?;
        let sync_file = file.try_clone().map_err(|error| {
            Fault::internal("failed to clone telemetry segment handle").with_source(error)
        })?;
        let mut writer = RecordWriter::new(BufWriter::new(file));
        writer.write(ENVELOPE_SCHEMA, 0, &encoded)?;
        writer.flush()?;
        sync_file.sync_data().map_err(|error| {
            Fault::internal("failed to synchronize telemetry segment").with_source(error)
        })?;
        state.next_sequence = state
            .next_sequence
            .checked_add(1)
            .ok_or_else(|| Fault::new(Code::OutOfRange, "telemetry sequence overflow"))?;
        self.files.publish(
            RelativePath::new("sequence").map_err(|error| Fault::internal(error.to_string()))?,
            state.next_sequence.to_string().as_bytes(),
            true,
        )?;
        Ok(envelope)
    }
    pub fn replay_after(&self, sequence: u64, limit: usize) -> FaultResult<Vec<Envelope>> {
        if limit == 0 || limit > 100_000 {
            return Err(Fault::invalid_argument("telemetry replay limit is invalid"));
        }
        let mut segments = segment_files(&self.root)?;
        segments.sort();
        let mut output = Vec::new();
        for path in segments {
            let file = fs::File::open(path).map_err(|error| {
                Fault::internal("failed to open telemetry segment").with_source(error)
            })?;
            let mut reader = RecordReader::new(
                BufReader::new(file),
                self.config
                    .maximum_event_bytes
                    .checked_add(ByteSize::new(4096))?,
            );
            while let Some(record) = reader.read_next()? {
                let maximum_payload = usize::try_from(self.config.maximum_event_bytes.get())
                    .map_err(|_| {
                        Fault::new(
                            Code::ResourceExhausted,
                            "telemetry event limit exceeds addressable memory",
                        )
                    })?;
                let envelope = Envelope::decode(&record.payload, maximum_payload)?;
                if envelope.sequence > sequence {
                    output.push(envelope);
                    if output.len() == limit {
                        return Ok(output);
                    }
                }
            }
        }
        Ok(output)
    }
    pub fn acknowledge(&self, sequence: u64) -> FaultResult<()> {
        let mut state = self
            .state
            .lock()
            .unwrap_or_else(std::sync::PoisonError::into_inner);
        if sequence < state.acknowledged || sequence >= state.next_sequence {
            return Err(Fault::invalid_argument(
                "telemetry acknowledgement is outside the valid sequence window",
            ));
        }
        state.acknowledged = sequence;
        self.files.publish(
            RelativePath::new("ack").map_err(|error| Fault::internal(error.to_string()))?,
            sequence.to_string().as_bytes(),
            true,
        )?;
        Ok(())
    }
    pub fn compact(&self) -> FaultResult<usize> {
        let acknowledged = self
            .state
            .lock()
            .unwrap_or_else(std::sync::PoisonError::into_inner)
            .acknowledged;
        let mut removed = 0_usize;
        for path in segment_files(&self.root)? {
            let max = max_sequence(&path, &self.config)?;
            if max.is_some_and(|value| value <= acknowledged) {
                fs::remove_file(path).map_err(|error| {
                    Fault::internal("failed to compact telemetry segment").with_source(error)
                })?;
                removed = removed.checked_add(1).ok_or_else(|| {
                    Fault::new(
                        Code::OutOfRange,
                        "telemetry compacted segment count overflow",
                    )
                })?;
            }
        }
        Ok(removed)
    }
}

fn segment_path(root: &Path, segment: u64) -> PathBuf {
    root.join(format!("segment-{segment:020}.mcrd"))
}

fn segment_files(root: &Path) -> FaultResult<Vec<PathBuf>> {
    let mut output = Vec::new();
    for entry in fs::read_dir(root)
        .map_err(|error| Fault::internal("failed to list telemetry spool").with_source(error))?
    {
        let entry = entry.map_err(|error| {
            Fault::internal("failed to inspect telemetry spool entry").with_source(error)
        })?;
        let name = entry.file_name().to_string_lossy().into_owned();
        if name.starts_with("segment-") && name.ends_with(".mcrd") {
            output.push(entry.path());
        }
    }
    Ok(output)
}

fn discover_segment(root: &Path) -> FaultResult<u64> {
    let mut maximum = 0_u64;
    for path in segment_files(root)? {
        if let Some(stem) = path
            .file_stem()
            .and_then(|value| value.to_str())
            .and_then(|value| value.strip_prefix("segment-"))
            .and_then(|value| value.parse::<u64>().ok())
        {
            maximum = maximum.max(stem);
        }
    }
    Ok(maximum)
}

fn read_counter(path: &Path) -> FaultResult<Option<u64>> {
    match fs::read_to_string(path) {
        Ok(value) => Ok(Some(value.trim().parse::<u64>().map_err(|error| {
            Fault::data_loss("telemetry counter is invalid").with_source(error)
        })?)),
        Err(error) if error.kind() == std::io::ErrorKind::NotFound => Ok(None),
        Err(error) => Err(Fault::internal("failed to read telemetry counter").with_source(error)),
    }
}

fn enforce_total_budget(root: &Path, limit: u64, additional: u64) -> FaultResult<()> {
    let used = segment_files(root)?
        .into_iter()
        .try_fold(0_u64, |total, path| {
            let size = fs::metadata(path)
                .map_err(|error| {
                    Fault::internal("failed to inspect telemetry segment").with_source(error)
                })?
                .len();
            total.checked_add(size).ok_or_else(|| {
                Fault::new(Code::OutOfRange, "telemetry spool size accounting overflow")
            })
        })?;
    let projected = used
        .checked_add(additional)
        .ok_or_else(|| Fault::new(Code::OutOfRange, "telemetry spool size accounting overflow"))?;
    if projected > limit {
        Err(Fault::new(
            Code::ResourceExhausted,
            "telemetry spool disk budget exceeded",
        ))
    } else {
        Ok(())
    }
}

fn max_sequence(path: &Path, config: &SpoolConfig) -> FaultResult<Option<u64>> {
    let file = fs::File::open(path)
        .map_err(|error| Fault::internal("failed to open telemetry segment").with_source(error))?;
    let mut reader = RecordReader::new(
        BufReader::new(file),
        config
            .maximum_event_bytes
            .checked_add(ByteSize::new(4096))?,
    );
    let maximum_payload = usize::try_from(config.maximum_event_bytes.get()).map_err(|_| {
        Fault::new(
            Code::ResourceExhausted,
            "telemetry event limit exceeds addressable memory",
        )
    })?;
    let mut maximum = None;
    while let Some(record) = reader.read_next()? {
        let envelope = Envelope::decode(&record.payload, maximum_payload)?;
        maximum =
            Some(maximum.map_or(envelope.sequence, |value: u64| value.max(envelope.sequence)));
    }
    Ok(maximum)
}
