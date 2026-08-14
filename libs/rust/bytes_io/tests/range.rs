use mindclade_bytes_io::ByteRange;

#[test] fn intersection_is_exact() {
    let a=ByteRange::new(10, 10).unwrap();
    let b=ByteRange::new(15, 20).unwrap();
    let i=a.intersection(b).unwrap().unwrap();
    assert_eq!(i.start(), 15);
    assert_eq!(i.length(), 5);
}
