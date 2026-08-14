use crate::DeviceCapability;
use mindclade_faults::{Fault, FaultResult};

pub fn from_inventory_line(line: &str) -> FaultResult<DeviceCapability> {
    parse_line("nvidia", line)
}

fn parse_line(vendor: &str, line: &str) -> FaultResult<DeviceCapability> {
    let fields: Vec<_> = line.split(',').map(str::trim).collect();
    if fields.len() != 2 {
        return Err(Fault::invalid_argument("NVIDIA inventory line must contain architecture,memory_bytes"));
    }
    let memory = fields[1]
        .parse::<u64>()
        .map_err(|error| Fault::invalid_argument("NVIDIA inventory memory is invalid").with_source(error))?;
    let capability = DeviceCapability { vendor: vendor.into(), architecture: fields[0].into(), total_memory_bytes: memory };
    capability.validate()?;
    Ok(capability)
}
