from dataclasses import dataclass
@dataclass(frozen=True)
class ToolchainRecord:
    tool: str; version: str; binary_digest: str; arguments_digest: str
