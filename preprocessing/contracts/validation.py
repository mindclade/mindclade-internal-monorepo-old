"""Shared preprocessing contract validation."""
def require_sha256(value:str,name:str)->None:
    if not value.startswith("sha256:") or len(value)!=71: raise ValueError(f"{name} must be canonical sha256 digest")
