use super::record::MolRecord;
use mindclade_faults::FaultResult;

pub fn serialize(record: &MolRecord) -> FaultResult<Vec<u8>> {
    record.validate()?;
    Ok(record.bytes.clone())
}
