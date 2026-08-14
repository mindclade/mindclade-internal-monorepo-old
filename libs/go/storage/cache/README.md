# cache

A provider-neutral ephemeral cache contract. Versions are monotonic per key,
TTL expiration is explicit, and conditional writes use `IfAbsent` or
`IfVersion`. Cache misses and version conflicts are structured faults.
