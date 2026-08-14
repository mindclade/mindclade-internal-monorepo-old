use mindclade_bytes_io::ByteSize;
use mindclade_runtime_core::ManualClock;
use mindclade_identifiers::ResourceId;
use mindclade_ipc::{
    Channel, Message, MessageKind
};
use std::io::Cursor;
use std::time::{
    Instant, SystemTime
};

#[test]
fn messages_round_trip_through_record_framing() {
    let clock = ManualClock::new(SystemTime::UNIX_EPOCH, Instant::now());
    let request = ResourceId::generate("request", &clock);
    assert!(request.is_ok());
    if let Ok(request) = request {
        let message = Message::new(request, 1, MessageKind::Request, "artifact.get", b"payload".to_vec(), ByteSize::new(1024));
        assert!(message.is_ok());
        if let Ok(message) = message {
            let reader = Cursor::new(Vec::<u8>::new());
            let writer = Cursor::new(Vec::<u8>::new());
            let channel = Channel::new(reader, writer, ByteSize::new(1024));
            assert!(channel.is_ok());
            if let Ok(mut channel) = channel {
                assert!(channel.send(&message).is_ok());
                let (_, writer) = channel.into_parts();
                let bytes = writer.into_inner();
                let channel = Channel::new(Cursor::new(bytes), Cursor::new(Vec::<u8>::new()), ByteSize::new(1024));
                if let Ok(mut channel) = channel {
                    assert_eq!(channel.receive().ok().flatten(), Some(message));
                }
            }
        }
    }
}
