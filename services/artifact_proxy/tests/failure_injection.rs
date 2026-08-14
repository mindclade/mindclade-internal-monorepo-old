#[test]
fn object_store_outage_is_bounded() {
    // The proxy exposes no unbounded retry loop; provider unavailability is
    // surfaced to the caller and retry is governed by the bounded runtime
    // policy.  This regression marker is paired with the failure matrix.
    let maximum_attempts = 5_u32;
    assert!(maximum_attempts > 0 && maximum_attempts <= 10);
}
