# Python rigid geometry

This package owns finite float64 three-dimensional rigid-geometry primitives: validated vectors,
point arrays and right-handed rotation matrices; immutable rigid transforms; frame construction;
axis-angle rotations; pairwise Euclidean distances; and RMSD. Inputs are copied into read-only
NumPy arrays, so caller mutation cannot alter a validated transform.

Rotations must be orthonormal with determinant +1 within an explicit bounded tolerance. Degenerate
axes and frames, non-finite coordinates, and incompatible shapes are rejected. Point operations
accept at most 1,000,000 points, and pairwise distances at most 4,000,000 output elements, avoiding
accidental quadratic allocation beyond that budget. Operations are deterministic for a fixed NumPy
build and input; callers that require a cross-platform numerical tolerance must state it in tests.

This package does not own molecular-domain semantics, coordinate-file formats, periodic boundary
conditions, neighbor-list algorithms, GPU kernels, unit conversion, or distributed execution.
Those belong to domain packages and accelerator adapters.
