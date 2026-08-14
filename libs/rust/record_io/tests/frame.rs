use mindclade_bytes_io::ByteSize;
use mindclade_record_io::{
    RecordReader, RecordWriter
};
#[test]fn frame_roundtrip() {
    let mut out=Vec::new();
    RecordWriter::new(&mut out).write(1, 0, b"abc").unwrap();
    let r=RecordReader::new(out.as_slice(), ByteSize::new(16)).read_next().unwrap().unwrap();
    assert_eq!(r.payload, b"abc");
}
