#![forbid(unsafe_code)]
// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

use mindclade_bytes_io::ByteRange;
use mindclade_content_digest::hash_bytes;
use mindclade_object_store::{LocalStore, ObjectPath, ObjectStore, PutCondition};
use mindclade_runtime_core::{Budget, ResourceVector};
use std::fs;
use std::hint::black_box;
use std::io::{Read, Write};
use std::os::unix::net::UnixStream;
use std::path::PathBuf;
use std::process::Command;
use std::sync::Arc;
use std::thread;
use std::time::{Instant, SystemTime, UNIX_EPOCH};

const PROBE_BYTES: usize = 32 * 1024 * 1024;
const RANGE_READS: usize = 64;
const CONTENDED_READERS: usize = 8;
const READS_PER_READER: usize = 8;

struct TemporaryDirectory(PathBuf);

impl TemporaryDirectory {
    fn create() -> Self {
        let nonce = SystemTime::now()
            .duration_since(UNIX_EPOCH)
            .expect("probe clock")
            .as_nanos();
        let path = std::env::temp_dir().join(format!(
            "mindclade-rust-perf-{}-{nonce}",
            std::process::id()
        ));
        fs::create_dir(&path).expect("create probe directory");
        Self(path)
    }
}

impl Drop for TemporaryDirectory {
    fn drop(&mut self) {
        let _ = fs::remove_dir_all(&self.0);
    }
}

fn main() {
    if std::env::args().any(|argument| argument == "--child-noop") {
        return;
    }

    let bytes = vec![0x5a_u8; PROBE_BYTES];
    // Warm page mappings, allocator state, and runtime CPU-feature detection
    // before collecting a median. A single cold sample is too sensitive to
    // concurrent CI load to serve as promotion evidence.
    black_box(hash_bytes(black_box(&bytes)));
    let mut verify_samples = [0.0_f64; 5];
    for sample in &mut verify_samples {
        let start = Instant::now();
        for _ in 0..4 {
            black_box(hash_bytes(black_box(&bytes)));
        }
        let seconds = start.elapsed().as_secs_f64();
        *sample = if seconds > 0.0 {
            128.0 / seconds
        } else {
            f64::INFINITY
        };
    }
    verify_samples.sort_by(f64::total_cmp);
    let verify_mib_per_s = verify_samples[verify_samples.len() / 2];

    let budget = Budget::root("probe", ResourceVector::default());
    let start = Instant::now();
    for _ in 0..20_000 {
        let reservation = budget
            .reserve(ResourceVector::default())
            .expect("zero reservation");
        black_box(reservation);
    }
    let reserve_us = start.elapsed().as_secs_f64() * 1_000_000.0 / 20_000.0;

    let ipc_mib_per_s = measure_ipc_throughput();
    let worker_start_p95_ms = measure_worker_startup();
    let (range_reads_per_s, contended_range_reads_per_s) = measure_verified_ranges(&bytes);
    let rss_bytes = resident_set_bytes();
    let fd_count = file_descriptor_count();

    println!(
        concat!(
            "{{",
            "\"artifact_verify_mib_per_s\":{:.6},",
            "\"runtime_host_invocation_overhead_us\":{:.6},",
            "\"unix_ipc_mib_per_s\":{:.6},",
            "\"node_stage_start_ms\":{:.6},",
            "\"verified_range_4k_ops_per_s\":{:.6},",
            "\"local_store_contended_4k_ops_per_s\":{:.6},",
            "\"perf_probe_rss_bytes\":{:.0},",
            "\"perf_probe_fd_count\":{:.0}",
            "}}"
        ),
        verify_mib_per_s,
        reserve_us,
        ipc_mib_per_s,
        worker_start_p95_ms,
        range_reads_per_s,
        contended_range_reads_per_s,
        rss_bytes,
        fd_count,
    );
}

/// Median of several sampled transfers, after a discarded warmup.
///
/// WAS a single cold sample, and it was the flakiest number this probe produced: three separate
/// agents saw `unix_ipc_mib_per_s` fail its 500 MiB/s floor at 476.6 and then 299.6, each A/B'd
/// it against a pristine tree on the same machine, and the pristine tree failed identically. On
/// a loaded host eight consecutive runs of an unchanged binary spread 86.7% -- 827 to 2120
/// MiB/s -- so the gate was reporting scheduler luck, not throughput.
///
/// `artifact_verify_mib_per_s` above already takes a median for exactly this reason and says so
/// in its own comment. This is that decision applied to the measurement that needed it most.
/// The warmup matters independently: the first transfer through a fresh socket pair pays for
/// the consumer thread's first schedule and for faulting in a 32 MiB payload that has only just
/// been written.
fn measure_ipc_throughput() -> f64 {
    // Discarded. Its cost is real but it is not what the budget is about.
    black_box(sample_ipc_throughput());
    let mut samples = [0.0_f64; 5];
    for sample in &mut samples {
        *sample = sample_ipc_throughput();
    }
    samples.sort_by(f64::total_cmp);
    samples[samples.len() / 2]
}

fn sample_ipc_throughput() -> f64 {
    let (mut writer, mut reader) = UnixStream::pair().expect("Unix socket pair");
    let repetitions = 4_usize;
    let consumer = thread::spawn(move || {
        let mut buffer = vec![0_u8; 64 * 1024];
        let mut received = 0_usize;
        let expected = PROBE_BYTES * repetitions;
        while received < expected {
            let count = reader.read(&mut buffer).expect("read IPC probe");
            assert!(count > 0, "IPC probe ended early");
            received += count;
        }
        black_box(received);
    });
    let payload = vec![0xa5_u8; PROBE_BYTES];
    let started = Instant::now();
    for _ in 0..repetitions {
        writer
            .write_all(black_box(&payload))
            .expect("write IPC probe");
    }
    // NotConnected here means the consumer already read every expected byte and dropped its
    // half before this call landed -- which is the success path, not a failure. Treating it as
    // fatal made the probe panic roughly one run in seven once the transfer was sampled six
    // times instead of once, so the qualification tool itself became a source of red unrelated
    // to throughput. Any other errno is still fatal.
    match writer.shutdown(std::net::Shutdown::Write) {
        Ok(()) => {}
        Err(error) if error.kind() == std::io::ErrorKind::NotConnected => {}
        Err(error) => panic!("close IPC probe: {error:?}"),
    }
    consumer.join().expect("IPC reader");
    let mib = usize_as_f64(PROBE_BYTES * repetitions) / (1024.0 * 1024.0);
    mib / started.elapsed().as_secs_f64()
}

fn measure_worker_startup() -> f64 {
    let executable = std::env::current_exe().expect("current probe executable");
    let mut samples = Vec::with_capacity(20);
    for _ in 0..20 {
        let started = Instant::now();
        let status = Command::new(&executable)
            .arg("--child-noop")
            .status()
            .expect("start probe child");
        assert!(status.success(), "probe child failed");
        samples.push(started.elapsed().as_secs_f64() * 1_000.0);
    }
    samples.sort_by(f64::total_cmp);
    samples[18]
}

fn measure_verified_ranges(bytes: &[u8]) -> (f64, f64) {
    let directory = TemporaryDirectory::create();
    let store = Arc::new(LocalStore::new(&directory.0).expect("local probe store"));
    let path = ObjectPath::new("probe/artifact.bin").expect("probe path");
    store
        .put(&path, bytes, PutCondition::CreateOnly)
        .expect("publish probe object");
    let range = ByteRange::new(1024, 4096).expect("probe range");

    black_box(store.get_range(&path, range).expect("warm verified range"));
    let started = Instant::now();
    for _ in 0..RANGE_READS {
        black_box(store.get_range(&path, range).expect("verified range"));
    }
    let range_reads_per_s = usize_as_f64(RANGE_READS) / started.elapsed().as_secs_f64();

    let started = Instant::now();
    let readers = (0..CONTENDED_READERS)
        .map(|_| {
            let store = Arc::clone(&store);
            let path = path.clone();
            thread::spawn(move || {
                for _ in 0..READS_PER_READER {
                    black_box(
                        store
                            .get_range(&path, range)
                            .expect("contended verified range"),
                    );
                }
            })
        })
        .collect::<Vec<_>>();
    for reader in readers {
        reader.join().expect("range reader");
    }
    let operations = CONTENDED_READERS * READS_PER_READER;
    let contended_range_reads_per_s = usize_as_f64(operations) / started.elapsed().as_secs_f64();
    (range_reads_per_s, contended_range_reads_per_s)
}

fn resident_set_bytes() -> f64 {
    if let Ok(status) = fs::read_to_string("/proc/self/status")
        && let Some(line) = status.lines().find(|line| line.starts_with("VmRSS:"))
        && let Some(kib) = line.split_whitespace().nth(1)
    {
        return kib.parse::<f64>().unwrap_or(0.0) * 1024.0;
    }
    Command::new("ps")
        .args(["-o", "rss=", "-p", &std::process::id().to_string()])
        .output()
        .ok()
        .filter(|output| output.status.success())
        .and_then(|output| String::from_utf8(output.stdout).ok())
        .and_then(|value| value.trim().parse::<f64>().ok())
        .map_or(0.0, |kib| kib * 1024.0)
}

fn file_descriptor_count() -> f64 {
    ["/proc/self/fd", "/dev/fd"]
        .into_iter()
        .find_map(|path| fs::read_dir(path).ok())
        .map_or(0.0, |entries| {
            usize_as_f64(entries.filter_map(Result::ok).count())
        })
}

fn usize_as_f64(value: usize) -> f64 {
    f64::from(u32::try_from(value).expect("portable probe count exceeds u32"))
}
