# Kubernetes patching

Merge-patch and server-side-apply helpers with optimistic locking, conflict
qualification, request metadata, and explicit field ownership. Mutation
functions are validated before provider calls; callers retain ownership of
higher-level object rollback semantics.
