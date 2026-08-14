# blob

Provider-neutral blob storage with canonical keys, immutable attributes,
conditional writes, range reads, integrity digests, and opaque pagination.
Implementations must map provider failures into `libs/go/faults` and must not
silently weaken precondition semantics.
