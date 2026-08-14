//! Local IPC framing and protocol negotiation.
#![forbid(unsafe_code)]

pub mod control;
pub mod descriptor;
pub mod framing;
pub mod shared_memory;
pub mod unix;
pub mod windows;
use mindclade_bytes_io::ByteSize;
use mindclade_content_digest::{
    hash_bytes, Digest
};
use mindclade_faults::{
    Code, Fault, FaultResult
};
use mindclade_identifiers::ResourceId;
use mindclade_record_io::{
    Decoder, Encoder, RecordReader, RecordWriter
};
use std::collections::BTreeSet;
use std::io::{
    Read, Write
};

pub const IPC_SCHEMA: u16 = 1;
pub const MAX_CONTROL_PAYLOAD: ByteSize = ByteSize::new(1024 * 1024);
const MAX_CAPABILITIES: usize = 256;
const MAX_CAPABILITY_BYTES: usize = 128;
const MAX_METHOD_BYTES: usize = 256;
const FRAME_OVERHEAD_BYTES: u64 = 4096;

fn valid_token(value: &str, maximum_bytes: usize) -> bool {
    !value.is_empty()
    && value.len() <= maximum_bytes
    && value == value.trim()
    && value.bytes().all(|byte| {
        byte.is_ascii_alphanumeric() || matches!(byte, b'.' | b'_' | b'-' | b'/' | b':')
    })
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct ProtocolVersion {
    pub major: u16,
    pub minor: u16,
}

impl ProtocolVersion {
    pub fn validate(self) -> FaultResult<()> {
        if self.major == 0 {
            return Err(Fault::invalid_argument("IPC protocol major version must be positive"));
        }
        Ok(())
    }
    pub fn negotiate(client: Self, server: Self) -> FaultResult<Self> {
        client.validate()?;
        server.validate()?;
        if client.major != server.major {
            return Err(Fault::new(
            Code::FailedPrecondition,
            "IPC major protocol versions are incompatible",
            ));
        }
        Ok(Self {
            major: client.major,
            minor: client.minor.min(server.minor),
        })
    }
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct Hello {
    pub version: ProtocolVersion,
    pub process_id: ResourceId,
    pub capabilities: Vec<String>,
}

impl Hello {
    pub fn validate(&self) -> FaultResult<()> {
        self.version.validate()?;
        if self.capabilities.len() > MAX_CAPABILITIES {
            return Err(Fault::new(
            Code::ResourceExhausted,
            "IPC capability count exceeds limit",
            ));
        }
        let mut previous: Option<&str> = None;
        let mut unique = BTreeSet::new();
        for capability in &self.capabilities {
            if !valid_token(capability, MAX_CAPABILITY_BYTES) {
                return Err(Fault::invalid_argument("IPC capability token is invalid"));
            }
            if !unique.insert(capability.as_str()) {
                return Err(Fault::invalid_argument("IPC capability list contains duplicates"));
            }
            if previous.is_some_and(|value| value >= capability.as_str()) {
                return Err(Fault::invalid_argument(
                "IPC capabilities must be strictly sorted for deterministic negotiation",
                ));
            }
            previous = Some(capability.as_str());
        }
        Ok(())
    }
    pub fn encode(&self) -> FaultResult<Vec<u8>> {
        self.validate()?;
        let mut encoder = Encoder::new();
        encoder.u16(IPC_SCHEMA);
        encoder.u16(self.version.major);
        encoder.u16(self.version.minor);
        encoder.string(&self.process_id.to_string())?;
        encoder.u32(
        u32::try_from(self.capabilities.len())
        .map_err(|_| Fault::new(Code::OutOfRange, "IPC capability count exceeds u32"))?,
        );
        for capability in &self.capabilities {
            encoder.string(capability)?;
        }
        Ok(encoder.into_bytes())
    }
    pub fn decode(bytes: &[u8]) -> FaultResult<Self> {
        let mut decoder = Decoder::new(bytes, 64 * 1024)?;
        if decoder.u16()? != IPC_SCHEMA {
            return Err(Fault::new(
            Code::FailedPrecondition,
            "IPC hello schema is unsupported",
            ));
        }
        let version = ProtocolVersion {
            major: decoder.u16()?,
            minor: decoder.u16()?,
        };
        let process_id = decoder
        .string()?
        .parse::<ResourceId>()
        .map_err(|error| Fault::data_loss("IPC process ID is invalid").with_source(error))?;
        let count = decoder.item_count()?;
        if count > MAX_CAPABILITIES {
            return Err(Fault::data_loss("IPC capability count exceeds limit"));
        }
        let mut capabilities = Vec::with_capacity(count);
        for _ in 0..count {
            capabilities.push(decoder.string()?.to_owned());
        }
        decoder.finish()?;
        let hello = Self {
            version,
            process_id,
            capabilities,
        };
        hello.validate()?;
        Ok(hello)
    }
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum MessageKind {
    Request = 1,
    Response = 2,
    Event = 3,
    Cancel = 4,
    Heartbeat = 5,
}

impl MessageKind {
    #[must_use]
    pub const fn code(self) -> u8 {
        match self {
            Self::Request => 1,
            Self::Response => 2,
            Self::Event => 3,
            Self::Cancel => 4,
            Self::Heartbeat => 5,
        }
    }
}

impl TryFrom<u8> for MessageKind {
    type Error = Fault;
    fn try_from(value: u8) -> FaultResult<Self> {
        match value {
            1 => Ok(Self::Request),
            2 => Ok(Self::Response),
            3 => Ok(Self::Event),
            4 => Ok(Self::Cancel),
            5 => Ok(Self::Heartbeat),
            _ => Err(Fault::data_loss("IPC message kind is invalid")),
        }
    }
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct Message {
    pub request_id: ResourceId,
    pub sequence: u64,
    pub kind: MessageKind,
    pub method: String,
    pub payload: Vec<u8>,
    pub payload_digest: Digest,
}

impl Message {
    pub fn validate(&self, maximum_payload: ByteSize) -> FaultResult<()> {
        if maximum_payload.get() == 0 || maximum_payload.get() > MAX_CONTROL_PAYLOAD.get() {
            return Err(Fault::invalid_argument(
            "IPC control payload limit must be positive and at most 1 MiB",
            ));
        }
        let payload_bytes = u64::try_from(self.payload.len())
        .map_err(|_| Fault::new(Code::OutOfRange, "IPC payload length exceeds u64"))?;
        if self.request_id.kind() != "request"
        || self.sequence == 0
        || !valid_token(&self.method, MAX_METHOD_BYTES)
        || payload_bytes > maximum_payload.get()
        {
            return Err(Fault::invalid_argument("IPC message fields exceed bounds"));
        }
        self.payload_digest.verify(&self.payload)
    }
    pub fn new(
    request_id: ResourceId,
    sequence: u64,
    kind: MessageKind,
    method: impl Into<String>,
    payload: Vec<u8>,
    maximum_payload: ByteSize,
    ) -> FaultResult<Self> {
        let message = Self {
            request_id,
            sequence,
            kind,
            method: method.into(),
            payload_digest: hash_bytes(&payload),
            payload,
        };
        message.validate(maximum_payload)?;
        Ok(message)
    }
    pub fn encode(&self) -> FaultResult<Vec<u8>> {
        self.payload_digest.verify(&self.payload)?;
        let mut encoder = Encoder::new();
        encoder.u16(IPC_SCHEMA);
        encoder.string(&self.request_id.to_string())?;
        encoder.u64(self.sequence);
        encoder.u8(self.kind.code());
        encoder.string(&self.method)?;
        encoder.bytes(self.payload_digest.as_bytes())?;
        encoder.bytes(&self.payload)?;
        Ok(encoder.into_bytes())
    }
    pub fn decode(bytes: &[u8], maximum_payload: ByteSize) -> FaultResult<Self> {
        if maximum_payload.get() == 0 || maximum_payload.get() > MAX_CONTROL_PAYLOAD.get() {
            return Err(Fault::invalid_argument("IPC payload limit is outside control-plane bounds"));
        }
        let frame_limit = maximum_payload
        .get()
        .checked_add(FRAME_OVERHEAD_BYTES)
        .ok_or_else(|| Fault::new(Code::OutOfRange, "IPC message limit overflow"))?;
        let limit = usize::try_from(frame_limit).map_err(|_| {
            Fault::new(
            Code::ResourceExhausted,
            "IPC message limit exceeds addressable memory",
            )
        })?;
        let mut decoder = Decoder::new(bytes, limit)?;
        if decoder.u16()? != IPC_SCHEMA {
            return Err(Fault::new(
            Code::FailedPrecondition,
            "IPC message schema is unsupported",
            ));
        }
        let request_id = decoder
        .string()?
        .parse::<ResourceId>()
        .map_err(|error| Fault::data_loss("IPC request ID is invalid").with_source(error))?;
        let sequence = decoder.u64()?;
        let kind = MessageKind::try_from(decoder.u8()?)?;
        let method = decoder.string()?.to_owned();
        let payload_digest = Digest::from_bytes(
        <[u8; 32]>::try_from(decoder.bytes()?)
        .map_err(|_| Fault::data_loss("IPC payload digest length is invalid"))?,
        );
        let payload = decoder.bytes()?.to_vec();
        decoder.finish()?;
        let message = Self {
            request_id,
            sequence,
            kind,
            method,
            payload,
            payload_digest,
        };
        message.validate(maximum_payload)?;
        Ok(message)
    }
}

pub struct Channel<R, W> {
    reader: RecordReader<R>,
    writer: RecordWriter<W>,
    maximum_payload: ByteSize,
}

impl<R: Read, W: Write> Channel<R, W> {
    pub fn new(reader: R, writer: W, maximum_payload: ByteSize) -> FaultResult<Self> {
        if maximum_payload.get() == 0 || maximum_payload.get() > MAX_CONTROL_PAYLOAD.get() {
            return Err(Fault::invalid_argument(
            "IPC control payload limit must be positive and at most 1 MiB; a bulk BufferDescriptor is required for larger data",
            ));
        }
        let frame_limit = maximum_payload.checked_add(ByteSize::new(FRAME_OVERHEAD_BYTES))?;
        Ok(Self {
            reader: RecordReader::new(reader, frame_limit),
            writer: RecordWriter::new(writer),
            maximum_payload,
        })
    }
    pub fn send(&mut self, message: &Message) -> FaultResult<()> {
        message.validate(self.maximum_payload)?;
        let bytes = message.encode()?;
        self.writer.write(IPC_SCHEMA, 0, &bytes)?;
        self.writer.flush()
    }
    pub fn receive(&mut self) -> FaultResult<Option<Message>> {
        match self.reader.read_next()? {
            Some(record) => Ok(Some(Message::decode(&record.payload, self.maximum_payload)?)),
            None => Ok(None),
        }
    }
    pub fn into_parts(self) -> (R, W) {
        (self.reader.into_inner(), self.writer.into_inner())
    }
}
