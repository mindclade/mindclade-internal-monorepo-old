use mindclade_runtime_core::ManualClock;
use mindclade_telemetry::{AttributeValue, Event, MemorySink, Severity, Sink};
use std::time::{Instant, SystemTime};

#[test]
fn events_are_bounded_and_secrets_are_redacted() {
    let clock = ManualClock::new(SystemTime::UNIX_EPOCH, Instant::now());
    let event = Event::new("checkpoint.committed", Severity::Info, &clock);
    assert!(event.is_ok());
    if let Ok(mut event) = event {
        assert!(event.attributes.insert("step", 42_u64));
        assert!(event.attributes.insert_redacted("token"));
        assert!(event.attributes.iter().any(|(key, value)| key == "token" && value == &AttributeValue::Redacted));
        let sink = MemorySink::default();
        assert!(sink.emit(&event).is_ok());
        assert_eq!(sink.snapshot().len(), 1);
    }
}
