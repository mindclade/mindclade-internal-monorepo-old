# lease

Fenced distributed leases. Renew and release require the complete lease token
and version, preventing a stale process from mutating ownership after expiry
and reacquisition.
