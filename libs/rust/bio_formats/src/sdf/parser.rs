use super::record::SdfRecord;
use mindclade_bounded_parse::{AllocationBudget, Cursor, Limits};
use mindclade_faults::{Code, Fault, FaultResult};

pub fn parse(bytes: &[u8], limits: Limits) -> FaultResult<Vec<SdfRecord>> {
    let mut cursor = Cursor::new(bytes, limits)?;
    let mut allocation = AllocationBudget::from_limits(limits);
    let mut output = Vec::new();
    let mut current = Vec::new();
    while let Some((_location, line)) = cursor.next_line()? {
        if line == b"$$$$" {
            if current.is_empty() { return Err(Fault::invalid_argument("SDF contains an empty record")); }
            if output.len() >= limits.maximum_records {
                return Err(Fault::new(Code::ResourceExhausted, "SDF record limit exceeded"));
            }
            let record = SdfRecord { bytes: std::mem::take(&mut current) };
            record.validate()?;
            output.push(record);
            continue;
        }
        allocation.charge_usize(line.len().checked_add(1).ok_or_else(|| Fault::new(Code::OutOfRange, "retained line allocation overflow"))?)?;
        current.extend_from_slice(line);
        current.push(b'\n');
    }
    if !current.is_empty() {
        if output.len() >= limits.maximum_records {
            return Err(Fault::new(Code::ResourceExhausted, "SDF record limit exceeded"));
        }
        let record = SdfRecord { bytes: current };
        record.validate()?;
        output.push(record);
    }
    if output.is_empty() { return Err(Fault::invalid_argument("SDF contains no records")); }
    Ok(output)
}
