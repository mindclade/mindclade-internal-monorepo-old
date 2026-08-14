# Google Cloud Storage Blob Adapter

`gcs` implements `blob.Store` using `cloud.google.com/go/storage`.

Uploads are staged to a temporary file before they are committed. This allows
Mindclade's SHA-256 contract and maximum-size policy to be enforced before a
new object generation becomes visible. Cloud Storage's native CRC32C handling
remains enabled as an additional transport-integrity check.

The caller owns the lifecycle of the `storage.Client` passed to `New`.
