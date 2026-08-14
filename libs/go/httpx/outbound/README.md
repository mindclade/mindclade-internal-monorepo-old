# Safe outbound HTTP

This is the required client path for webhooks, HTTP ingestion, partner callbacks,
and external evaluation submissions.

```go
client, err := outbound.NewClient(outbound.Policy{
    AllowedHosts:      []string{"partner.example.com"},
    AllowedPorts:      []string{"443"},
    AllowedMediaTypes: []string{"application/json"},
    HTTPSOnly:         true,
    MaxResponseBytes:  4 << 20,
    Timeout:           20 * time.Second,
})
resp, err := client.Do(req)
```

It revalidates DNS/literal addresses and redirects, rejects loopback/private/
link-local/metadata addresses by default, bounds connections, TLS/headers/body,
and enforces media/encoding policy. Domain signing, webhook state, and tenant
allowlist policy stay in the consuming domain.
