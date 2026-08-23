// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

//! Cross-language Mindclade resource IDs: `<kind>_<32 lowercase UUIDv7 hex>`.
//!
//! The grammar constants below are the Rust half of a three-language contract.
//! `libs/go/identifiers` spells the same numbers as `MinimumKindLength`,
//! `MaximumKindLength`, `IDSeparator` and `UUIDCompactLength`;
//! `libs/python/identifiers/resource.py` spells them as `MINIMUM_KIND_LENGTH`,
//! `MAXIMUM_KIND_LENGTH`, `ID_SEPARATOR` and `UUID_COMPACT_LENGTH`. They are
//! named rather than inlined so
//! `tests/integration/cross_language/test_identifiers.py` can read all three out
//! of source and fail when one drifts.

use core::fmt;
use core::str::FromStr;
use mindclade_runtime_core::Clock;
use std::fs::File;
use std::io::Read;
use std::sync::Mutex;
use std::time::UNIX_EPOCH;

/// The one character that separates a kind prefix from an ID body.
///
/// It is also the character a kind may never contain: a kind carrying this byte
/// produces text no parser in any of the three languages can split back into the
/// pair it came from.
pub const ID_SEPARATOR: char = '_';
/// Shortest admissible kind prefix.
pub const MINIMUM_KIND_LENGTH: usize = 2;
/// Longest admissible kind prefix.
pub const MAXIMUM_KIND_LENGTH: usize = 24;
/// Hexadecimal characters in an ID body — a compact 16-byte UUID.
pub const ID_BODY_LENGTH: usize = 32;

const HEX: &[u8; 16] = b"0123456789abcdef";
const UUID_V7_MAX_MILLIS: u64 = (1_u64 << 48) - 1;

/// Random bytes consumed per identifier.
///
/// A version 7 UUID's random field is bytes 6..16 — 80 bits, six of which are
/// then overwritten by the four-bit version nibble in byte 6 and the two variant
/// bits in byte 8, leaving 74. It is the same field
/// `libs/go/identifiers/generator.go` fills with `io.ReadFull(entropy, uuid[6:])`.
const ID_ENTROPY_BYTES: usize = 10;

/// Bytes drawn from the operating system per refill.
///
/// Fixed and never grown: the repository's bounded-buffer rule applied to an
/// entropy pool. It amortizes the device read across 51 identifiers without
/// letting the pool's memory depend on the mint rate.
const ENTROPY_POOL_BYTES: usize = 512;

/// The operating system CSPRNG.
///
/// Reaching `getrandom(2)` directly needs either a third-party crate — neither
/// `getrandom` nor `rand` is admitted by `libs/rust/third_party_crates.json` —
/// or an `unsafe` `libc` call, and this crate is `#![forbid(unsafe_code)]`.
///
/// It is the same generator behind that syscall but not the same contract, and
/// the difference is worth stating rather than glossing: `getrandom(2)` blocks
/// until the kernel pool is seeded, this device does not. A process minting
/// identifiers in the first moments of a freshly provisioned machine's boot can
/// therefore draw from an unseeded pool. Admitting `getrandom` as a dependency
/// is the fix; until then the exposure is bounded to pre-seed boot, which is
/// still strictly better than the process counter this replaced.
///
/// A sandbox that does not mount `/dev` makes the open fail and minting fail
/// with it. That is the correct direction — an identifier with no entropy behind
/// it is worse than no identifier — but it is a new way for `generate` to fail
/// and callers inherit it.
const RANDOM_DEVICE: &str = "/dev/urandom";

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct ResourceIdError(&'static str);
impl fmt::Display for ResourceIdError {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.write_str(self.0)
    }
}
impl std::error::Error for ResourceIdError {}

/// Canonical identifier compatible with `libs/go/identifiers.ID`.
#[derive(Clone, Eq, Hash, Ord, PartialEq, PartialOrd)]
pub struct ResourceId {
    kind: String,
    body: String,
}

impl ResourceId {
    /// Mints an identifier of `kind` stamped at `clock`'s wall-clock time.
    ///
    /// The random field comes from the operating system CSPRNG. It used to come
    /// from a public bijective mix over `(nanoseconds since the epoch, a process
    /// counter starting at 1, the pid)`, every input of which is either
    /// published in the identifier itself or cheaply enumerable — see
    /// `tests/entropy.rs`, which reproduces that construction and asserts the
    /// mint no longer matches it. Callers name real things with these:
    /// `libs/rust/atomic_fs` builds `.<body>.partial` paths in a shared
    /// directory from one, and `telemetry_spool`, `checkpoint_io` and
    /// `telemetry` mint durable identity from it.
    ///
    /// Uniqueness is probabilistic over the 74-bit random field, as it is for
    /// `libs/go/identifiers` across processes. Unlike Go's `Generator` and
    /// Python's `IdGenerator`, this call keeps no per-mint state, so it makes no
    /// intra-millisecond ordering promise.
    pub fn generate(kind: &str, clock: &dyn Clock) -> Result<Self, ResourceIdError> {
        validate_kind(kind)?;
        let duration = clock
            .system_now()
            .duration_since(UNIX_EPOCH)
            .map_err(|_| ResourceIdError("resource ID clock is before Unix epoch"))?;
        let millis = u64::try_from(duration.as_millis())
            .map_err(|_| ResourceIdError("resource ID timestamp exceeds u64"))?;
        if millis > UUID_V7_MAX_MILLIS {
            return Err(ResourceIdError(
                "resource ID timestamp exceeds UUIDv7 48-bit domain",
            ));
        }
        let mut uuid = [0_u8; 16];
        uuid[0..6].copy_from_slice(&millis.to_be_bytes()[2..8]);
        random_bytes(&mut uuid[6..16])?;
        uuid[6] = (uuid[6] & 0x0f) | 0x70;
        uuid[8] = (uuid[8] & 0x3f) | 0x80;
        Ok(Self {
            kind: kind.to_owned(),
            body: encode_hex(uuid),
        })
    }
    pub fn parse(value: &str) -> Result<Self, ResourceIdError> {
        let Some((kind, body)) = value.split_once(ID_SEPARATOR) else {
            return Err(ResourceIdError("resource ID is missing its separator"));
        };
        if body.contains(ID_SEPARATOR) {
            return Err(ResourceIdError(
                "resource ID contains more than one separator",
            ));
        }
        validate_kind(kind)?;
        if body.len() != ID_BODY_LENGTH
            || body
                .bytes()
                .any(|byte| !matches!(byte, b'0'..=b'9' | b'a'..=b'f'))
        {
            return Err(ResourceIdError(
                "resource ID body must be 32 lowercase hexadecimal characters",
            ));
        }
        let bytes = decode_hex(body)?;
        if bytes[6] >> 4 != 7 || bytes[8] & 0xc0 != 0x80 {
            return Err(ResourceIdError(
                "resource ID body must be an RFC variant UUIDv7",
            ));
        }
        Ok(Self {
            kind: kind.to_owned(),
            body: body.to_owned(),
        })
    }
    #[must_use]
    pub fn kind(&self) -> &str {
        &self.kind
    }
    #[must_use]
    pub fn body(&self) -> &str {
        &self.body
    }
}

impl fmt::Display for ResourceId {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(f, "{}{ID_SEPARATOR}{}", self.kind, self.body)
    }
}
impl fmt::Debug for ResourceId {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.debug_tuple("ResourceId")
            .field(&self.to_string())
            .finish()
    }
}
impl FromStr for ResourceId {
    type Err = ResourceIdError;
    fn from_str(value: &str) -> Result<Self, Self::Err> {
        Self::parse(value)
    }
}

/// The single kind grammar for this crate.
///
/// `ResourceKind` validates through here rather than carrying its own copy. The
/// copy it used to carry admitted `_` and `-`, a leading digit, one-character
/// kinds and lengths up to 48 — so `ResourceKind::parse("runtime_host")`
/// succeeded and the identifier built from it, `runtime_host_<32 hex>`, was
/// rejected by this module's own parser, by `libs/go/identifiers.ParseID` and by
/// `libs/python/identifiers.ResourceId.parse`. A kind that cannot survive the
/// identifier it prefixes is not a kind.
pub(crate) fn validate_kind(value: &str) -> Result<(), ResourceIdError> {
    let mut bytes = value.bytes();
    let Some(first) = bytes.next() else {
        return Err(ResourceIdError("resource ID kind must not be empty"));
    };
    if !(MINIMUM_KIND_LENGTH..=MAXIMUM_KIND_LENGTH).contains(&value.len())
        || !first.is_ascii_lowercase()
        || !bytes.all(|byte| byte.is_ascii_lowercase() || byte.is_ascii_digit())
    {
        return Err(ResourceIdError(
            "resource ID kind must be 2-24 lowercase alphanumeric characters starting with a letter",
        ));
    }
    Ok(())
}

/// A fixed window onto the operating system CSPRNG.
struct EntropyPool {
    source: File,
    buffer: [u8; ENTROPY_POOL_BYTES],
    cursor: usize,
}

impl EntropyPool {
    fn open() -> Result<Self, ResourceIdError> {
        let source = File::open(RANDOM_DEVICE)
            .map_err(|_| ResourceIdError("resource ID entropy source is unavailable"))?;
        Ok(Self {
            source,
            buffer: [0; ENTROPY_POOL_BYTES],
            // Start spent so the first mint reads the device rather than
            // handing out a zeroed buffer.
            cursor: ENTROPY_POOL_BYTES,
        })
    }

    fn fill(&mut self, output: &mut [u8]) -> Result<(), ResourceIdError> {
        if output.len() > ENTROPY_POOL_BYTES - self.cursor {
            self.source
                .read_exact(&mut self.buffer)
                .map_err(|_| ResourceIdError("resource ID entropy source read failed"))?;
            self.cursor = 0;
        }
        let end = self.cursor + output.len();
        output.copy_from_slice(&self.buffer[self.cursor..end]);
        // Issued bytes are cleared rather than left addressable in a buffer that
        // lives as long as the process.
        self.buffer[self.cursor..end].fill(0);
        self.cursor = end;
        Ok(())
    }
}

static ENTROPY: Mutex<Option<EntropyPool>> = Mutex::new(None);

fn random_bytes(output: &mut [u8]) -> Result<(), ResourceIdError> {
    debug_assert!(output.len() <= ID_ENTROPY_BYTES);
    let mut guard = ENTROPY.lock().unwrap_or_else(|poisoned| {
        // A panic between the device read and the cursor update could leave
        // already-issued bytes ahead of the cursor. Dropping the pool costs one
        // open and rules that out; handing the same bytes to two identifiers is
        // the one failure a unique identifier may not have.
        //
        // The poison flag is cleared as well. Rust's flag is sticky, so leaving
        // it set would send every later mint down this branch — reopening the
        // device and reading 512 bytes to serve 10, for the life of the process.
        ENTROPY.clear_poison();
        let mut guard = poisoned.into_inner();
        *guard = None;
        guard
    });
    let filled = {
        let pool = match guard.as_mut() {
            Some(pool) => pool,
            None => guard.insert(EntropyPool::open()?),
        };
        pool.fill(output)
    };
    if filled.is_err() {
        // A failed device read drops the handle rather than keeping it. An fd
        // that has gone bad — EIO, a sandbox that revoked `/dev` after the open
        // — would otherwise fail every subsequent mint in the process forever,
        // when one reopen would have recovered.
        *guard = None;
    }
    filled
}

fn encode_hex(bytes: [u8; 16]) -> String {
    let mut output = String::with_capacity(ID_BODY_LENGTH);
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
