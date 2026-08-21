# Serving / Testing

**Status:** implemented test support library.

This test-only package provides validated fixtures, deterministic model and gateway doubles,
canonical JSON golden comparison, and a bounded concurrent load harness with exact success/failure
accounting and latency percentiles. Histories, operations, worker threads, nesting, and collection
sizes all have explicit ceilings.

These tools make no production network calls and never rewrite a golden automatically. The load
harness is suitable for behavioral regression and local micro-load tests; it is not a substitute
for connected fleet, accelerator, chaos, or capacity qualification.
