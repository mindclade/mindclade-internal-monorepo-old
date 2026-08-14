//! Cross-language Mindclade resource IDs: `<kind>_<32 lowercase UUIDv7 hex>`.

use core::fmt;
use core::str::FromStr;
use mindclade_runtime_core::Clock;
use std::process;
use std::sync::atomic::{AtomicU64, Ordering};
use std::time::UNIX_EPOCH;

static COUNTER: AtomicU64 = AtomicU64::new(1);
const HEX: &[u8; 16] = b"0123456789abcdef";
const UUID_V7_MAX_MILLIS: u64 = (1_u64 << 48) - 1;

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct ResourceIdError(&'static str);
impl fmt::Display for ResourceIdError {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result { f.write_str(self.0) }
}
impl std::error::Error for ResourceIdError {}

/// Canonical identifier compatible with `libs/go/identifiers.ID`.
#[derive(Clone, Eq, Hash, Ord, PartialEq, PartialOrd)]
pub struct ResourceId {
    kind: String,
    body: String,
}

impl ResourceId {
    pub fn generate(kind: &str, clock: &dyn Clock) -> Result<Self, ResourceIdError> {
        validate_kind(kind)?;
        let duration = clock.system_now().duration_since(UNIX_EPOCH)
            .map_err(|_| ResourceIdError("resource ID clock is before Unix epoch"))?;
        let millis = u64::try_from(duration.as_millis())
            .map_err(|_| ResourceIdError("resource ID timestamp exceeds u64"))?;
        if millis > UUID_V7_MAX_MILLIS {
            return Err(ResourceIdError("resource ID timestamp exceeds UUIDv7 48-bit domain"));
        }
        let counter = COUNTER.fetch_update(Ordering::SeqCst, Ordering::SeqCst, |current| current.checked_add(1))
            .map_err(|_| ResourceIdError("resource ID process counter exhausted"))?;
        let pid = u128::from(process::id());
        let seed = duration.as_nanos() ^ u128::from(counter).rotate_left(37) ^ pid.rotate_left(83);
        let mut uuid = mix128(seed).to_be_bytes();
        let millis_bytes = millis.to_be_bytes();
        uuid[0..6].copy_from_slice(&millis_bytes[2..8]);
        uuid[6] = (uuid[6] & 0x0f) | 0x70;
        uuid[8] = (uuid[8] & 0x3f) | 0x80;
        Ok(Self { kind: kind.to_owned(), body: encode_hex(uuid) })
    }
    pub fn parse(value: &str) -> Result<Self, ResourceIdError> {
        let Some((kind, body)) = value.split_once('_') else {
            return Err(ResourceIdError("resource ID is missing its separator"));
        };
        if body.contains('_') { return Err(ResourceIdError("resource ID contains more than one separator")); }
        validate_kind(kind)?;
        if body.len() != 32 || body.bytes().any(|byte| !matches!(byte, b'0'..=b'9' | b'a'..=b'f')) {
            return Err(ResourceIdError("resource ID body must be 32 lowercase hexadecimal characters"));
        }
        let bytes = decode_hex(body)?;
        if bytes[6] >> 4 != 7 || bytes[8] & 0xc0 != 0x80 {
            return Err(ResourceIdError("resource ID body must be an RFC variant UUIDv7"));
        }
        Ok(Self { kind: kind.to_owned(), body: body.to_owned() })
    }
    #[must_use] pub fn kind(&self) -> &str { &self.kind }
    #[must_use] pub fn body(&self) -> &str { &self.body }
}

impl fmt::Display for ResourceId {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result { write!(f, "{}_{}", self.kind, self.body) }
}
impl fmt::Debug for ResourceId {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result { f.debug_tuple("ResourceId").field(&self.to_string()).finish() }
}
impl FromStr for ResourceId {
    type Err = ResourceIdError;
    fn from_str(value: &str) -> Result<Self, Self::Err> { Self::parse(value) }
}

fn validate_kind(value: &str) -> Result<(), ResourceIdError> {
    let mut bytes = value.bytes();
    let Some(first) = bytes.next() else { return Err(ResourceIdError("resource ID kind must not be empty")); };
    if !(2..=24).contains(&value.len())
        || !first.is_ascii_lowercase()
        || !bytes.all(|byte| byte.is_ascii_lowercase() || byte.is_ascii_digit())
    {
        return Err(ResourceIdError("resource ID kind must be 2-24 lowercase alphanumeric characters starting with a letter"));
    }
    Ok(())
}

fn mix128(mut value: u128) -> u128 {
    value ^= value >> 30;
    value = value.wrapping_mul(0xbf58_476d_1ce4_e5b9_bf58_476d_1ce4_e5b9);
    value ^= value >> 27;
    value = value.wrapping_mul(0x94d0_49bb_1331_11eb_94d0_49bb_1331_11eb);
    value ^ (value >> 31)
}

fn encode_hex(bytes: [u8; 16]) -> String {
    let mut output = String::with_capacity(32);
    for byte in bytes {
        output.push(char::from(HEX[usize::from(byte >> 4)]));
        output.push(char::from(HEX[usize::from(byte & 0x0f)]));
    }
    output
}

fn decode_hex(value: &str) -> Result<[u8; 16], ResourceIdError> {
    fn nibble(byte: u8) -> Option<u8> {
        match byte {
            b'0'..=b'9' => Some(byte - b'0'),
            b'a'..=b'f' => Some(byte - b'a' + 10),
            _ => None,
        }
    }
    let mut output = [0_u8; 16];
    for (index, pair) in value.as_bytes().chunks_exact(2).enumerate() {
        output[index] = (nibble(pair[0]).ok_or(ResourceIdError("invalid hex"))? << 4)
            | nibble(pair[1]).ok_or(ResourceIdError("invalid hex"))?;
    }
    Ok(output)
}
