# Fuzz targets

Connected Rust qualification materializes cargo-fuzz targets for FASTA, FASTQ,
A3M, Stockholm, PDB/mmCIF, SDF/MOL, record frames, manifests, route snapshots,
and worker protocol frames. The source tree intentionally contains the corpus
contract even when cargo-fuzz is unavailable in the offline scaffold environment.
