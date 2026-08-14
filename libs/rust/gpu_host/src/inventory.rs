use crate::{DeviceCapability, device::DeviceId};
use mindclade_faults::{Code, Fault, FaultResult};
use std::collections::BTreeSet;

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct AcceleratorDevice {
    pub id: DeviceId,
    pub capability: DeviceCapability,
}

#[derive(Clone, Debug, Default)]
pub struct Inventory {
    devices: Vec<AcceleratorDevice>,
}
impl Inventory {
    pub fn new(devices: Vec<AcceleratorDevice>) -> FaultResult<Self> {
        if devices.is_empty() {
            return Err(Fault::new(Code::NotFound, "no accelerator devices discovered"));
        }
        let mut identities = BTreeSet::new();
        for device in &devices {
            device.capability.validate()?;
            if device.id.vendor != device.capability.vendor || !identities.insert((device.id.vendor.clone(), device.id.ordinal)) {
                return Err(Fault::data_loss("accelerator inventory contains duplicate or inconsistent device identity"));
            }
        }
        Ok(Self { devices })
    }
    #[must_use]
    pub fn devices(&self) -> &[AcceleratorDevice] {
        &self.devices
    }
}
