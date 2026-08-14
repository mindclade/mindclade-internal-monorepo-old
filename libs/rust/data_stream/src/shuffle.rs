//! Deterministic Fisher-Yates shuffling with checked index conversion.

use mindclade_faults::{Code, Fault, FaultResult};

pub fn deterministic_shuffle<T>(values: &mut [T], seed: u64) -> FaultResult<()> {
    let mut state = seed;
    for index in (1..values.len()).rev() {
        state = splitmix64(state);
        let modulus = u128::try_from(index)
            .map_err(|_| Fault::new(Code::OutOfRange, "shuffle index exceeds u128"))?
            .checked_add(1)
            .ok_or_else(|| Fault::new(Code::OutOfRange, "shuffle modulus overflow"))?;
        let target = usize::try_from(u128::from(state) % modulus)
            .map_err(|_| Fault::new(Code::OutOfRange, "shuffle target exceeds usize"))?;
        values.swap(index, target);
    }
    Ok(())
}

fn splitmix64(mut value: u64) -> u64 {
    value = value.wrapping_add(0x9e37_79b9_7f4a_7c15);
    value = (value ^ (value >> 30)).wrapping_mul(0xbf58_476d_1ce4_e5b9);
    value = (value ^ (value >> 27)).wrapping_mul(0x94d0_49bb_1331_11eb);
    value ^ (value >> 31)
}
