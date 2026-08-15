"""Cache eligibility policy; scientific freshness remains explicit."""

from dataclasses import dataclass

from .store import Entry


@dataclass(frozen=True)
class CachePolicy:
    require_qualified: bool = True
    accepted_producer_versions: frozenset[str] = frozenset()

    def accepts(self, entry: Entry) -> bool:
        return (not self.require_qualified or entry.qualified) and (
            not self.accepted_producer_versions
            or entry.producer_version in self.accepted_producer_versions
        )
