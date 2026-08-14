use mindclade_faults::{
    Code, Fault, WireFault
};

#[test] fn sensitive_context_never_crosses_wire() {
    let fault=Fault::new(Code::Internal, "x").with_sensitive_context("secret");
    let wire=WireFault::from(&fault);
    assert_eq!(wire.context[0].value, "[REDACTED]");
}
