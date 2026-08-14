# Resource versions

`resourceversion` is the canonical optimistic-concurrency mechanism for mutable
control-plane resources. It provides monotonic versions, existence/match
preconditions, parsing/encoding, and strong HTTP ETags.

Handlers map `If-Match`/`If-None-Match` or Protobuf preconditions into the same
contract. Repositories use conditional updates and return conflict or failed
precondition rather than silently overwriting. Blob-provider generations remain
in the blob namespace; they are not coerced into this type.
