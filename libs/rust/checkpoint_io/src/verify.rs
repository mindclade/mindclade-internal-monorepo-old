pub use crate::reader::VerificationReport;
use crate::CheckpointReader;
use mindclade_faults::{
    Fault, FaultResult
};
pub fn require_valid(reader: &CheckpointReader, id: &str) -> FaultResult<VerificationReport> {
    let report=reader.verify(id)?;
    if !report.is_valid() {
        return Err(Fault::data_loss(format!("checkpoint verification failed: {} shard(s)", report.failures.len())));
    }
    Ok(report)
}
