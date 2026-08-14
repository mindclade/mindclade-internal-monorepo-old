use mindclade_bio_formats::{
    a3m, fasta, fastq, stockholm
};
use mindclade_bounded_parse::{
    Limits, ParseMode
};

#[test]fn fasta_roundtrip() {
    let r=fasta::parse(b">x desc\nACGT\n", Limits::default(), ParseMode::Strict).unwrap();
    let bytes=fasta::serialize(&r, 80).unwrap();
    assert_eq!(fasta::parse(&bytes, Limits::default(), ParseMode::Strict).unwrap(), r);
}

#[test]fn a3m_preserves_insertions() {
    let r=a3m::parse(b">x\nACdEF-\n", Limits::default(), ParseMode::Strict).unwrap();
    assert_eq!(r[0].sequence, b"ACdEF-");
}

#[test]fn fastq_roundtrip() {
    let r=fastq::parse(b"@x\nAC\n+\nII\n", Limits::default(), ParseMode::Strict).unwrap();
    let b=fastq::serialize(&r).unwrap();
    assert_eq!(fastq::parse(&b, Limits::default(), ParseMode::Strict).unwrap(), r);
}

#[test]fn stockholm_roundtrip() {
    let r=stockholm::parse(b"# STOCKHOLM 1.0\nx AC\n//\n", Limits::default()).unwrap();
    let b=stockholm::serialize(&r).unwrap();
    assert_eq!(stockholm::parse(&b, Limits::default()).unwrap(), r);
}
