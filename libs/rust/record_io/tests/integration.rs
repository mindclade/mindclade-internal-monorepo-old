use mindclade_bytes_io::ByteSize;
use mindclade_record_io::{Decoder, Encoder, RecordReader, RecordWriter};
use std::io::Cursor;

#[test]
fn frames_and_codec_round_trip() {
    let mut encoder = Encoder::new();
    assert!(encoder.string("checkpoint").is_ok());
    encoder.u64(42);
    let payload = encoder.into_bytes();
    let mut writer = RecordWriter::new(Vec::new());
    assert!(writer.write(1, 0, &payload).is_ok());
    let bytes = writer.into_inner();
    let mut reader = RecordReader::new(Cursor::new(bytes), ByteSize::new(1024));
    let record = reader.read_next();
    assert!(record.is_ok());
    if let Ok(Some(record)) = record {
        let mut decoder = match Decoder::new(&record.payload, 1024) { Ok(value) => value, Err(_) => { assert!(false); return; } };
        assert_eq!(decoder.string().ok(), Some("checkpoint"));
        assert_eq!(decoder.u64().ok(), Some(42));
        assert!(decoder.finish().is_ok());
    }
}
