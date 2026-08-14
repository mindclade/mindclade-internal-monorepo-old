# Service configuration

`config` resolves strict immutable service configuration from ordered sources.
It provides field catalogs, required/default/secret/reloadable metadata,
unknown-key rejection, value validation, source provenance, deterministic
digests, redaction, atomic last-known-good snapshots, and restart-required
reporting.

```go
loader, err := config.New([]config.Field{
    {Key: "SERVICE_NAME", Required: true},
    {Key: "DATABASE_DSN", Required: true, Secret: true},
    {Key: "LOG_LEVEL", Default: config.String("info"), Reloadable: true},
}, defaults, fileSource, config.EnvSource{Mapping: envMapping})
if err != nil { return err }

snapshot, err := loader.Load(ctx)
if err != nil { return err }
atomic, err := config.NewAtomic(snapshot)
```

Composition roots decode the snapshot into service-specific typed settings.
Domain packages never read environment variables directly. Secrets are redacted
from diagnostics; nonreloadable changes retain last-known-good and require a
restart.
