use crate::common::{parse_header, push_sequence, FastqRecord};
use mindclade_bounded_parse::{AllocationBudget, Cursor, Limits, ParseMode};
use mindclade_faults::{Code, Fault, FaultResult};

pub fn parse(bytes: &[u8], limits: Limits, mode: ParseMode) -> FaultResult<Vec<FastqRecord>> {
    let mut cursor = Cursor::new(bytes, limits)?;
    let mut allocation = AllocationBudget::from_limits(limits);
    let mut total_tokens = 0_usize;
    let mut output = Vec::new();
    loop {
        let Some((_location, header)) = cursor.next_line()? else { break; };
        if header.is_empty() { continue; }
        if !header.starts_with(b"@") {
            return Err(Fault::invalid_argument("FASTQ header must start with @"));
        }
        let (_, sequence_line) = cursor.next_line()?.ok_or_else(|| Fault::invalid_argument("FASTQ sequence is missing"))?;
        let (_, plus) = cursor.next_line()?.ok_or_else(|| Fault::invalid_argument("FASTQ + line is missing"))?;
        let (_, quality) = cursor.next_line()?.ok_or_else(|| Fault::invalid_argument("FASTQ quality is missing"))?;
        if !plus.starts_with(b"+") {
            return Err(Fault::invalid_argument("FASTQ separator must start with +"));
        }
        if output.len() >= limits.maximum_records {
            return Err(Fault::new(Code::ResourceExhausted, "FASTQ record limit exceeded"));
        }
        let (id, description) = parse_header(&header[1..], "FASTQ", &mut allocation)?;
        let mut sequence = Vec::new();
        push_sequence(
            &mut sequence,
            sequence_line,
            &mut total_tokens,
            limits.maximum_tokens,
            false,
            mode == ParseMode::Strict,
            &mut allocation,
        )?;
        allocation.charge_usize(quality.len())?;
        let record = FastqRecord { id, description, sequence, quality: quality.to_vec() };
        record.validate()?;
        output.push(record);
    }
    if output.is_empty() {
        return Err(Fault::invalid_argument("FASTQ contains no records"));
    }
    Ok(output)
}
