# Signed keyset pagination

`pagination` creates opaque signed cursors bound to tenant/workspace, resource
kind, normalized filter digest, sort definition, last key, schema version, and
expiry. Repositories translate the validated cursor into a keyset query; the
package never builds SQL.

Use stable deterministic sort keys with a canonical ID tie-breaker. Reject a
cursor when scope, filter, ordering, version, signature, or expiry differs.
Avoid offset pagination for mutable run/job/artifact/dataset/audit collections.
