use super::record::PdbRecord;
use mindclade_faults::FaultResult;

pub fn serialize(records: &[PdbRecord]) -> FaultResult<Vec<u8>> {
    let mut output = Vec::new();
    for record in records {
        record.validate()?;
        output.extend_from_slice(&record.line);
        output.push(b'\n');
    }
    Ok(output)
}
