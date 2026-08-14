use mindclade_faults::Fault;
#[derive(Clone,Debug,Eq,PartialEq)]pub struct BridgeError{pub code:String,pub message:String}
impl From<Fault> for BridgeError{fn from(value:Fault)->Self{Self{code:value.code().to_string(),message:value.message().to_owned()}}}
