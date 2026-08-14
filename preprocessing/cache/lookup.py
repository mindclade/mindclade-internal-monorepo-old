from .policy import CachePolicy
from .store import Store,Entry
def lookup(store:Store,key:str,policy:CachePolicy)->Entry|None:
    entry=store.get(key);return entry if entry is not None and policy.accepts(entry) else None
