from .store import Entry
def promote(entry:Entry)->Entry:return Entry(entry.key,entry.artifact,entry.producer_version,True)
