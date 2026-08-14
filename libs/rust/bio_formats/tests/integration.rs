use mindclade_bio_formats::{
    a3m, fasta, fastq, mmcif, mol, parse_fasta, parse_stockholm, pdb, sdf
};
use mindclade_bounded_parse::{
    Limits, ParseMode
};
use mindclade_bytes_io::ByteSize;

#[test]
fn fasta_a3m_fastq_and_stockholm_round_trip() {
    let limits = Limits::default();
    let fasta_records = parse_fasta(b">p1 desc\nACDX\n", limits, ParseMode::Strict).expect("fasta");
    assert_eq!(fasta_records[0].sequence, b"ACDX");
    let encoded = fasta::serialize(&fasta_records, 80).expect("serialize");
    assert_eq!(parse_fasta(&encoded, limits, ParseMode::Strict).expect("reparse"), fasta_records);
    let a3m_records = a3m::parse(b">q\nACde-F\n", limits, ParseMode::Strict).expect("a3m");
    assert_eq!(a3m_records[0].sequence, b"ACde-F");
    let fastq_records = fastq::parse(b"@r1\nACGT\n+\nIIII\n", limits, ParseMode::Strict).expect("fastq");
    let fastq_encoded = fastq::serialize(&fastq_records).expect("serialize fastq");
    assert_eq!(fastq::parse(&fastq_encoded, limits, ParseMode::Strict).expect("reparse fastq"), fastq_records);
    let stockholm = parse_stockholm(b"# STOCKHOLM 1.0\np1 AC-D\np1 EF--\n//\n", limits).expect("stockholm");
    assert_eq!(stockholm[0].sequence, b"AC-DEF--");
}

#[test]
fn structural_formats_are_bounded_and_lossless() {
    let limits = Limits::default();
    let pdb_records = pdb::parse(b"ATOM      1  CA  ALA A   1\nEND\n", limits).expect("pdb");
    assert!(pdb::serialize(&pdb_records).expect("pdb serialize").starts_with(b"ATOM"));
    let mol_bytes = b"example\n\
  Mindclade\n\
comment\n\
  1  0  0  0  0  0            999 V2000\n\
    0.0000    0.0000    0.0000 C   0  0  0  0  0  0  0  0  0  0  0  0\n\
M  END\n";
    let mol_record = mol::parse(mol_bytes, limits).expect("mol");
    assert_eq!(mol::serialize(&mol_record).expect("mol serialize"), mol_bytes);
    let sdf_bytes = [mol_bytes.as_slice(), b">  <NAME>\nexample\n\n$$$$\n"].concat();
    let sdf_records = sdf::parse(&sdf_bytes, limits).expect("sdf");
    assert_eq!(sdf_records.len(), 1);
    assert!(sdf::serialize(&sdf_records).expect("sdf serialize").ends_with(b"$$$$\n"));
}

#[test]
fn mmcif_supports_quoted_and_semicolon_text_fields() {
    let input = b"data_demo\n_entry.id demo\n_struct.title 'demo structure'\n;\nmultiline\nvalue\n;\n";
    let document = mmcif::parse(input, Limits::default()).expect("mmcif");
    assert!(document.tokens.iter().any(|token| token.value == "demo structure"));
    assert!(document.tokens.iter().any(|token| token.value.contains("multiline\nvalue")));
    let encoded = mmcif::serialize(&document).expect("serialize mmcif");
    let reparsed = mmcif::parse(&encoded, Limits::default()).expect("reparse mmcif");
    assert!(!reparsed.tokens.is_empty());
}

#[test]
fn retained_allocation_budget_is_enforced() {
    let limits = Limits {
        maximum_input_bytes: ByteSize::new(64),
        maximum_line_bytes: 64,
        maximum_records: 8,
        maximum_tokens: 64,
        maximum_metadata_entries: 8,
        maximum_nesting: 8,
        maximum_allocation_bytes: ByteSize::new(16),
    };
    let input = b">identifier-long\nACGTACGTACGT\n";
    assert!(parse_fasta(input, limits, ParseMode::Strict).is_err());
}

#[test]
fn malformed_inputs_fail_closed() {
    let limits = Limits::default();
    assert!(fastq::parse(b"@r\nACG\n+\nII\n", limits, ParseMode::Strict).is_err());
    assert!(parse_stockholm(b"# STOCKHOLM 1.0\np1 AC\n", limits).is_err());
    assert!(mmcif::parse(b"data_demo\n;unterminated\n", limits).is_err());
    assert!(mol::parse(b"not-a-mol\n", limits).is_err());
    assert!(sdf::parse(b"$$$$\n", limits).is_err());
}
