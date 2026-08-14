use mindclade_python_bridge::PythonBridge;

#[test]
fn safe_bridge_exposes_deterministic_primitives() {
    assert_eq!(PythonBridge::sha256(b"abc"), "sha256:ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad");
    let tokens = PythonBridge::protein_tokens(b"ACDX", 4);
    assert!(tokens.is_ok());
    assert!(PythonBridge::protein_tokens(b"ACDX", 3).is_err());
}
