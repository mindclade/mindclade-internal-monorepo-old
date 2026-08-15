// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

//! Deterministic, bounded tokenizer runtime contracts.
#![forbid(unsafe_code)]

use mindclade_faults::{Code, Fault, FaultResult};
use std::collections::BTreeMap;

pub type TokenId = u32;
const MAX_TOKENIZER_NAME_BYTES: usize = 128;
const MAX_VOCABULARY_SIZE: usize = 1_000_000;
const MAX_TOKEN_BYTES: usize = 4_096;

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct Encoding {
    pub ids: Vec<TokenId>,
    pub offsets: Vec<(u32, u32)>,
}

impl Encoding {
    pub fn validate(&self) -> FaultResult<()> {
        if self.ids.len() != self.offsets.len() {
            return Err(Fault::data_loss(
                "token IDs and offsets have different lengths",
            ));
        }
        let mut previous = 0_u32;
        for (start, end) in &self.offsets {
            if start > end || *start < previous {
                return Err(Fault::data_loss("token offsets are invalid"));
            }
            previous = *end;
        }
        Ok(())
    }
}

pub trait Tokenizer: Send + Sync {
    fn name(&self) -> &str;
    fn vocabulary_size(&self) -> usize;
    fn encode(&self, input: &[u8], maximum_tokens: usize) -> FaultResult<Encoding>;
    fn decode(&self, ids: &[TokenId], maximum_bytes: usize) -> FaultResult<Vec<u8>>;
}

#[derive(Clone, Debug, Default)]
pub struct ByteTokenizer;

impl Tokenizer for ByteTokenizer {
    fn name(&self) -> &str {
        "byte-v1"
    }
    fn vocabulary_size(&self) -> usize {
        256
    }
    fn encode(&self, input: &[u8], maximum_tokens: usize) -> FaultResult<Encoding> {
        require_offset_domain(input.len())?;
        if input.len() > maximum_tokens {
            return Err(Fault::new(
                Code::ResourceExhausted,
                "token output exceeds limit",
            ));
        }
        let ids = input.iter().map(|value| u32::from(*value)).collect();
        let mut offsets = Vec::with_capacity(input.len());
        for index in 0..input.len() {
            offsets.push(unit_offset(index)?);
        }
        let encoding = Encoding { ids, offsets };
        encoding.validate()?;
        Ok(encoding)
    }
    fn decode(&self, ids: &[TokenId], maximum_bytes: usize) -> FaultResult<Vec<u8>> {
        if ids.len() > maximum_bytes {
            return Err(Fault::new(
                Code::ResourceExhausted,
                "decoded output exceeds limit",
            ));
        }
        ids.iter()
            .map(|value| {
                u8::try_from(*value)
                    .map_err(|_| Fault::invalid_argument("byte token is out of range"))
            })
            .collect()
    }
}

#[derive(Clone, Debug)]
pub struct AlphabetTokenizer {
    name: String,
    alphabet: Vec<u8>,
    by_byte: BTreeMap<u8, TokenId>,
    unknown: Option<TokenId>,
}

impl AlphabetTokenizer {
    pub fn new(name: impl Into<String>, alphabet: &[u8], unknown: Option<u8>) -> FaultResult<Self> {
        let name = name.into();
        validate_name(&name)?;
        if alphabet.is_empty() || alphabet.len() > usize::from(u16::MAX) {
            return Err(Fault::invalid_argument("alphabet size is invalid"));
        }
        let mut by_byte = BTreeMap::new();
        for (index, byte) in alphabet.iter().copied().enumerate() {
            let token_id = u32::try_from(index)
                .map_err(|_| Fault::new(Code::OutOfRange, "alphabet token ID exceeds u32"))?;
            if by_byte.insert(byte, token_id).is_some() {
                return Err(Fault::invalid_argument(
                    "alphabet contains duplicate symbols",
                ));
            }
        }
        let unknown = match unknown {
            Some(byte) => Some(
                *by_byte
                    .get(&byte)
                    .ok_or_else(|| Fault::invalid_argument("unknown symbol is not in alphabet"))?,
            ),
            None => None,
        };
        Ok(Self {
            name,
            alphabet: alphabet.to_vec(),
            by_byte,
            unknown,
        })
    }
    pub fn protein() -> FaultResult<Self> {
        Self::new("protein-v1", b"ACDEFGHIKLMNPQRSTVWYXBZUO-", Some(b'X'))
    }
    pub fn nucleic_acid() -> FaultResult<Self> {
        Self::new("nucleic-acid-v1", b"ACGTUN-", Some(b'N'))
    }
}

impl Tokenizer for AlphabetTokenizer {
    fn name(&self) -> &str {
        &self.name
    }
    fn vocabulary_size(&self) -> usize {
        self.alphabet.len()
    }
    fn encode(&self, input: &[u8], maximum_tokens: usize) -> FaultResult<Encoding> {
        require_offset_domain(input.len())?;
        if input.len() > maximum_tokens {
            return Err(Fault::new(
                Code::ResourceExhausted,
                "token output exceeds limit",
            ));
        }
        let mut ids = Vec::with_capacity(input.len());
        let mut offsets = Vec::with_capacity(input.len());
        for (index, byte) in input.iter().copied().enumerate() {
            let normalized = byte.to_ascii_uppercase();
            let id = self
                .by_byte
                .get(&normalized)
                .copied()
                .or(self.unknown)
                .ok_or_else(|| {
                    Fault::invalid_argument(
                        "input contains a symbol outside the tokenizer alphabet",
                    )
                })?;
            ids.push(id);
            offsets.push(unit_offset(index)?);
        }
        let encoding = Encoding { ids, offsets };
        encoding.validate()?;
        Ok(encoding)
    }
    fn decode(&self, ids: &[TokenId], maximum_bytes: usize) -> FaultResult<Vec<u8>> {
        if ids.len() > maximum_bytes {
            return Err(Fault::new(
                Code::ResourceExhausted,
                "decoded output exceeds limit",
            ));
        }
        ids.iter()
            .map(|id| {
                let index = usize::try_from(*id)
                    .map_err(|_| Fault::invalid_argument("token ID exceeds platform limits"))?;
                self.alphabet
                    .get(index)
                    .copied()
                    .ok_or_else(|| Fault::invalid_argument("token ID is outside the alphabet"))
            })
            .collect()
    }
}

#[derive(Clone, Debug)]
pub struct VocabularyTokenizer {
    name: String,
    tokens: Vec<Vec<u8>>,
    by_token: BTreeMap<Vec<u8>, TokenId>,
    unknown: Option<TokenId>,
}

impl VocabularyTokenizer {
    pub fn new(
        name: impl Into<String>,
        tokens: Vec<Vec<u8>>,
        unknown: Option<TokenId>,
    ) -> FaultResult<Self> {
        let name = name.into();
        validate_name(&name)?;
        if tokens.is_empty() || tokens.len() > MAX_VOCABULARY_SIZE {
            return Err(Fault::invalid_argument("vocabulary size is invalid"));
        }
        let mut by_token = BTreeMap::new();
        for (index, token) in tokens.iter().enumerate() {
            if token.is_empty() || token.len() > MAX_TOKEN_BYTES {
                return Err(Fault::invalid_argument(
                    "vocabulary token length is invalid",
                ));
            }
            let token_id = u32::try_from(index)
                .map_err(|_| Fault::new(Code::OutOfRange, "vocabulary token ID exceeds u32"))?;
            if by_token.insert(token.clone(), token_id).is_some() {
                return Err(Fault::invalid_argument(
                    "vocabulary contains duplicate tokens",
                ));
            }
        }
        if unknown
            .is_some_and(|value| usize::try_from(value).map_or(true, |index| index >= tokens.len()))
        {
            return Err(Fault::invalid_argument("unknown token ID is invalid"));
        }
        Ok(Self {
            name,
            tokens,
            by_token,
            unknown,
        })
    }
}

impl Tokenizer for VocabularyTokenizer {
    fn name(&self) -> &str {
        &self.name
    }
    fn vocabulary_size(&self) -> usize {
        self.tokens.len()
    }
    fn encode(&self, input: &[u8], maximum_tokens: usize) -> FaultResult<Encoding> {
        require_offset_domain(input.len())?;
        let mut ids = Vec::new();
        let mut offsets = Vec::new();
        let mut cursor = 0_usize;
        while cursor < input.len() {
            while cursor < input.len() && input[cursor].is_ascii_whitespace() {
                cursor += 1;
            }
            if cursor == input.len() {
                break;
            }
            let start = cursor;
            while cursor < input.len() && !input[cursor].is_ascii_whitespace() {
                cursor += 1;
            }
            let end = cursor;
            if ids.len() >= maximum_tokens {
                return Err(Fault::new(
                    Code::ResourceExhausted,
                    "token output exceeds limit",
                ));
            }
            let token = &input[start..end];
            let id = self
                .by_token
                .get(token)
                .copied()
                .or(self.unknown)
                .ok_or_else(|| {
                    Fault::invalid_argument("input contains an unknown vocabulary token")
                })?;
            ids.push(id);
            offsets.push((offset_u32(start)?, offset_u32(end)?));
        }
        let encoding = Encoding { ids, offsets };
        encoding.validate()?;
        Ok(encoding)
    }
    fn decode(&self, ids: &[TokenId], maximum_bytes: usize) -> FaultResult<Vec<u8>> {
        let mut output = Vec::new();
        for (index, id) in ids.iter().enumerate() {
            let token_index = usize::try_from(*id)
                .map_err(|_| Fault::invalid_argument("token ID exceeds platform limits"))?;
            let token = self
                .tokens
                .get(token_index)
                .ok_or_else(|| Fault::invalid_argument("token ID is outside the vocabulary"))?;
            let separator = usize::from(index > 0);
            let next = output
                .len()
                .checked_add(separator)
                .and_then(|size| size.checked_add(token.len()))
                .ok_or_else(|| Fault::new(Code::OutOfRange, "decoded output length overflow"))?;
            if next > maximum_bytes {
                return Err(Fault::new(
                    Code::ResourceExhausted,
                    "decoded output exceeds limit",
                ));
            }
            if index > 0 {
                output.push(b' ');
            }
            output.extend_from_slice(token);
        }
        Ok(output)
    }
}

fn validate_name(name: &str) -> FaultResult<()> {
    if name.is_empty() || name.len() > MAX_TOKENIZER_NAME_BYTES || name != name.trim() {
        return Err(Fault::invalid_argument("tokenizer name is invalid"));
    }
    Ok(())
}

fn require_offset_domain(length: usize) -> FaultResult<()> {
    let length = u64::try_from(length)
        .map_err(|_| Fault::new(Code::OutOfRange, "tokenizer input length exceeds u64"))?;
    if length > u64::from(u32::MAX) {
        return Err(Fault::new(
            Code::ResourceExhausted,
            "tokenizer input exceeds u32 offset domain",
        ));
    }
    Ok(())
}

fn offset_u32(value: usize) -> FaultResult<u32> {
    u32::try_from(value)
        .map_err(|_| Fault::new(Code::ResourceExhausted, "token offset exceeds u32 domain"))
}

fn unit_offset(index: usize) -> FaultResult<(u32, u32)> {
    let start = offset_u32(index)?;
    let end = start
        .checked_add(1)
        .ok_or_else(|| Fault::new(Code::ResourceExhausted, "token offset exceeds u32 domain"))?;
    Ok((start, end))
}
