use super::record::PdbRecord;
use mindclade_bounded_parse::{AllocationBudget, Cursor, Limits};
use mindclade_faults::{Code, Fault, FaultResult};

pub fn parse(bytes: &[u8], limits: Limits) -> FaultResult<Vec<PdbRecord>> {
    let mut cursor = Cursor::new(bytes, limits)?;
    let mut allocation = AllocationBudget::from_limits(limits);
    let mut output = Vec::new();
    while let Some((_location, line)) = cursor.next_line()? {
        if line.is_empty() { continue; }
        if output.len() >= limits.maximum_records {
            return Err(Fault::new(Code::ResourceExhausted, "PDB line limit exceeded"));
        }
        if line.iter().any(|value| *value == 0 || (!value.is_ascii() && !value.is_ascii_whitespace())) {
            return Err(Fault::invalid_argument("PDB line contains non-ASCII or NUL data"));
        }
        let name = std::str::from_utf8(&line[..line.len().min(6)])
            .map_err(|error| Fault::invalid_argument("PDB record name is not UTF-8").with_source(error))?
            .trim()
            .to_owned();
        if name.is_empty() || name.len() > 6 {
            return Err(Fault::invalid_argument("PDB record name is invalid"));
        }
        allocation.charge_usize(name.len().checked_add(line.len()).ok_or_else(|| Fault::new(Code::OutOfRange, "PDB retained allocation overflow"))?)?;
        let record = PdbRecord { record_name: name, line: line.to_vec() };
        record.validate()?;
        output.push(record);
    }
    if output.is_empty() { return Err(Fault::invalid_argument("PDB contains no records")); }
    Ok(output)
}
