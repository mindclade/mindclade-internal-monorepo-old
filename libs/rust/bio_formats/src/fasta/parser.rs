use crate::common::{parse_header, push_sequence, SequenceRecord};
use mindclade_bounded_parse::{AllocationBudget, Cursor, Limits, ParseMode};
use mindclade_faults::{Code, Fault, FaultResult};

pub fn parse(bytes: &[u8], limits: Limits, mode: ParseMode) -> FaultResult<Vec<SequenceRecord>> {
    let mut cursor = Cursor::new(bytes, limits)?;
    let mut allocation = AllocationBudget::from_limits(limits);
    let mut total_tokens = 0_usize;
    let mut output = Vec::new();
    let mut current: Option<SequenceRecord> = None;
    while let Some((_location, line)) = cursor.next_line()? {
        if line.is_empty() { continue; }
        if line[0] == b'>' {
            if let Some(record) = current.take() {
                record.validate()?;
                output.push(record);
            }
            if output.len() >= limits.maximum_records {
                return Err(Fault::new(Code::ResourceExhausted, "FASTA record limit exceeded"));
            }
            let (id, description) = parse_header(&line[1..], "FASTA", &mut allocation)?;
            current = Some(SequenceRecord { id, description, sequence: Vec::new() });
        } else if let Some(record) = current.as_mut() {
            push_sequence(
                &mut record.sequence,
                line,
                &mut total_tokens,
                limits.maximum_tokens,
                false,
                mode == ParseMode::Strict,
                &mut allocation,
            )?;
        } else {
            return Err(Fault::invalid_argument("FASTA sequence appears before a header"));
        }
    }
    if let Some(record) = current {
        record.validate()?;
        output.push(record);
    }
    if output.is_empty() {
        return Err(Fault::invalid_argument("FASTA contains no records"));
    }
    Ok(output)
}
