# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Behavioural coverage for the preprocessing cache: keys, store, policy, lookup, promotion.

A preprocessing cache is a correctness surface, not a speed knob. Every assertion in this file
is about one of two silent failures: serving a result that was computed from inputs other than
the ones the caller asked about (a key that is not content-bound), or failing to serve a result
that was already proven (a key that is not stable under irrelevant variation). Neither shows up
as an exception at runtime — the first returns confident wrong numbers and the second returns
correct numbers after paying for a search again — so both are pinned here.

Several tests below are labelled DOCUMENTED CURRENT BEHAVIOUR. Those record what the shipped
implementation does today where that is surprising or weaker than the surrounding tree's
conventions. They are deliberately not fixes: changing the implementation is a contract change
with cache-invalidation consequences, and it does not belong in a coverage change.
"""

from __future__ import annotations

from dataclasses import FrozenInstanceError

import pytest

from preprocessing.cache import (
    CachePolicy,
    Entry,
    MemoryStore,
    cache_key,
    feature_bundle_key,
    msa_search_key,
    template_search_key,
)
from preprocessing.cache.lookup import lookup
from preprocessing.cache.promotion import promote
from preprocessing.contracts import ArtifactRef


def _digest(marker: str) -> str:
    """A syntactically valid sha256 digest, distinct per single-character marker."""
    return "sha256:" + marker * 64


def _artifact(marker: str = "a") -> ArtifactRef:
    return ArtifactRef(_digest(marker), 128, "application/json", "features", 1)


def _entry(
    *,
    key: str = "sha256:" + "0" * 64,
    producer_version: str = "p1",
    qualified: bool = True,
) -> Entry:
    return Entry(key, _artifact(), producer_version, qualified)


class _RecordingStore:
    """A second `Store` implementation, so the protocol seam is exercised by something that is
    not `MemoryStore`.

    `put_if_absent` raises rather than recording, because "lookup does not write" is a property
    worth failing on rather than asserting after the fact.
    """

    def __init__(self, entry: Entry | None) -> None:
        self._entry = entry
        self.reads: list[str] = []

    def get(self, key: str) -> Entry | None:
        self.reads.append(key)
        return self._entry

    def put_if_absent(self, entry: Entry) -> Entry:
        raise AssertionError("lookup must not write to the store")


# --------------------------------------------------------------------------------------------
# keys.py — cache_key
# --------------------------------------------------------------------------------------------


def test_cache_key_is_a_prefixed_lowercase_sha256_hex_digest() -> None:
    # The "sha256:" prefix is a contract, not decoration. These keys are stored and logged
    # beside `ArtifactRef.digest` values, which carry the same prefix and are validated for it,
    # so the hash algorithm is never implicit in either direction and a future algorithm change
    # is a visible change in the string rather than a same-shaped value that means something
    # else.
    key = cache_key("ns/v1", digests=[_digest("a")], fields={"k": "v"})
    prefix, separator, hexdigest = key.partition(":")

    assert prefix == "sha256"
    assert separator == ":"
    assert len(hexdigest) == 64
    assert set(hexdigest) <= set("0123456789abcdef")


def test_cache_key_wire_format_is_pinned_to_a_golden_value() -> None:
    # The key names entries in a cache that outlives any single process, so the canonical
    # payload (its member names, its separators, its sort order, its ASCII escaping) is a wire
    # format. Changing any of those breaks nothing loudly: it invalidates every stored entry at
    # once and silently repays for work that was already proven. A literal is the only
    # assertion that catches that — re-deriving the expectation inside the test would move with
    # the code it is supposed to hold still.
    assert (
        cache_key("golden/v1", digests=[_digest("b"), _digest("a")], fields={"z": 1, "a": "x"})
        == "sha256:bef9ec72125ad968d8bfc5862c2d1b4dcd687ac7c82f91556b8069cf729c0757"
    )


def test_cache_key_rejects_an_empty_namespace() -> None:
    # Without a namespace every caller shares one key space, and two different computations
    # over the same digests collide into one entry. Fail loudly at construction rather than
    # produce a key that is well-formed and unsafe.
    with pytest.raises(ValueError, match="cache namespace required"):
        cache_key("", digests=[_digest("a")])


def test_cache_key_is_stable_under_digest_and_field_reordering() -> None:
    # Callers assemble digests and fields by iterating sets, dicts and query results whose
    # order is not part of the computation. If that order leaked into the key, the same work
    # would land under a new key on the next process and the cache would approach a zero hit
    # rate without ever being wrong — the hardest kind of regression to notice.
    first, second, third = _digest("1"), _digest("2"), _digest("3")

    assert cache_key(
        "ns/v1", digests=[first, second, third], fields={"x": "1", "y": "2"}
    ) == cache_key("ns/v1", digests=[third, first, second], fields={"y": "2", "x": "1"})


def test_cache_key_normalizes_order_without_deduplicating() -> None:
    # `sorted` is a normalization, not a set. A bundle built over two copies of an artifact is
    # a different bundle from one built over a single copy, so multiplicity has to survive.
    repeated = _digest("1")

    assert cache_key("ns/v1", digests=[repeated]) != cache_key(
        "ns/v1", digests=[repeated, repeated]
    )


def test_cache_key_namespaces_partition_the_key_space() -> None:
    # The namespace carries the payload's schema version ("/v1"), which is what lets the shape
    # of a cached value change without a migration: v2 simply cannot read v1's entries.
    digests = [_digest("1")]
    fields = {"k": "v"}
    keys = {
        cache_key(namespace, digests=digests, fields=fields)
        for namespace in ("msa-search/v1", "template-search/v1", "feature-bundle/v1", "ns/v2")
    }

    assert len(keys) == 4


def test_cache_key_field_values_carry_their_json_type() -> None:
    # 0, False and "0" are three different field values and JSON renders them three different
    # ways. Python would treat the first two as equal; the key must not, because a policy flag
    # that changes from an integer to a boolean changes the computation.
    keys = {cache_key("ns/v1", fields={"strict": value}) for value in (0, False, "0")}

    assert len(keys) == 3


def test_cache_key_field_boundaries_cannot_be_forged() -> None:
    # JSON quoting, not string concatenation, is what separates one field from the next. Under
    # a naive join ("mm" + "seqs2.1") and ("mmseqs" + "2.1") would produce identical bytes, and
    # a result from one tool would be served for a request naming another.
    assert cache_key("ns/v1", fields={"tool": "mm", "tool_version": "seqs2.1"}) != cache_key(
        "ns/v1", fields={"tool": "mmseqs", "tool_version": "2.1"}
    )


def test_cache_key_defaults_match_explicit_empties() -> None:
    # The defaults are part of the key space: a caller that omits `fields` and one that passes
    # an empty mapping are describing the same computation and must hit the same entry.
    assert cache_key("ns/v1") == cache_key("ns/v1", digests=(), fields=None)
    assert cache_key("ns/v1") == cache_key("ns/v1", digests=[], fields={})


def test_cache_key_does_not_encode_which_role_a_digest_played() -> None:
    # DOCUMENTED CURRENT BEHAVIOUR, not an endorsement. `cache_key` flattens every digest into
    # one sorted list, so the key records a *set* of digests and forgets which argument each
    # arrived in: swapping the entity digest with the parameters digest yields the same key for
    # a materially different search. It cannot bite while the roles draw from disjoint content
    # — an entity digest is never also a parameter-blob digest — but that safety is a property
    # of the callers, not something the key enforces. Pinned so that adding positional binding
    # shows up here as a deliberate, cache-invalidating change rather than as a quiet one.
    entity, database, parameters = _digest("1"), _digest("2"), _digest("3")

    assert msa_search_key(entity, "mmseqs", "2.1", database, parameters) == msa_search_key(
        parameters, "mmseqs", "2.1", database, entity
    )


def test_cache_key_silently_accepts_a_bare_string_as_a_sequence_of_digests() -> None:
    # DOCUMENTED CURRENT BEHAVIOUR. `str` satisfies `Sequence[str]`, so passing one digest
    # where a list of digests is expected type-checks, and then hashes that digest's individual
    # characters. The result is a well-formed key bound to nothing the caller meant, with no
    # exception and nothing in the output to distinguish it from a real key.
    assert cache_key("ns/v1", digests="ab") == cache_key("ns/v1", digests=["a", "b"])
    assert cache_key("ns/v1", digests="ab") != cache_key("ns/v1", digests=["ab"])


# --------------------------------------------------------------------------------------------
# keys.py — the three named key constructors
# --------------------------------------------------------------------------------------------


def test_msa_search_key_pins_its_namespace_and_field_names() -> None:
    # Restated rather than derived: the namespace string and the field names are what bind
    # every persisted MSA entry, so renaming either has to fail a test instead of quietly
    # orphaning the cache.
    composed = cache_key(
        "msa-search/v1",
        digests=[_digest("d"), _digest("e"), _digest("f")],
        fields={"tool": "mmseqs", "tool_version": "2.1"},
    )

    assert msa_search_key(_digest("d"), "mmseqs", "2.1", _digest("e"), _digest("f")) == composed
    assert composed == "sha256:56e5f74415118ab71fff0c8734d7a485a7db633331e924e531fc47aa4af63cfd"


def test_msa_search_key_is_sensitive_to_every_input() -> None:
    # All five arguments are inputs to the search. A tool-version bump or a reference-database
    # snapshot roll that did not move the key would keep serving alignments computed under the
    # superseded one, which is the specific way a stale MSA reaches a model unnoticed.
    variants = [
        (_digest("1"), "mmseqs", "2.1", _digest("2"), _digest("3")),
        (_digest("4"), "mmseqs", "2.1", _digest("2"), _digest("3")),
        (_digest("1"), "jackhmmer", "2.1", _digest("2"), _digest("3")),
        (_digest("1"), "mmseqs", "2.2", _digest("2"), _digest("3")),
        (_digest("1"), "mmseqs", "2.1", _digest("5"), _digest("3")),
        (_digest("1"), "mmseqs", "2.1", _digest("2"), _digest("6")),
    ]
    keys = [msa_search_key(*arguments) for arguments in variants]

    assert len(set(keys)) == len(keys)


def test_template_search_key_pins_its_namespace_and_carries_the_date_cutoff() -> None:
    # `max_template_date` is the anti-leakage cutoff: the same query against the same database
    # with a later cutoff is a different, more permissive search. It is a field rather than a
    # digest, so this is the assertion that keeps it inside the key at all.
    composed = cache_key(
        "template-search/v1",
        digests=[_digest("1"), _digest("2"), _digest("3"), _digest("4")],
        fields={"max_template_date": "2021-09-30", "tool_version": "2.1"},
    )

    assert (
        template_search_key(
            _digest("1"), _digest("2"), _digest("3"), "2021-09-30", _digest("4"), "2.1"
        )
        == composed
    )


def test_template_search_key_is_sensitive_to_every_input() -> None:
    variants = [
        (_digest("1"), _digest("2"), _digest("3"), "2021-09-30", _digest("4"), "2.1"),
        (_digest("5"), _digest("2"), _digest("3"), "2021-09-30", _digest("4"), "2.1"),
        (_digest("1"), _digest("6"), _digest("3"), "2021-09-30", _digest("4"), "2.1"),
        (_digest("1"), _digest("2"), _digest("7"), "2021-09-30", _digest("4"), "2.1"),
        (_digest("1"), _digest("2"), _digest("3"), "2022-01-01", _digest("4"), "2.1"),
        (_digest("1"), _digest("2"), _digest("3"), "2021-09-30", _digest("8"), "2.1"),
        (_digest("1"), _digest("2"), _digest("3"), "2021-09-30", _digest("4"), "2.2"),
    ]
    keys = [template_search_key(*arguments) for arguments in variants]

    assert len(set(keys)) == len(keys)


def test_feature_bundle_key_pins_its_namespace_and_counts_its_artifacts() -> None:
    # The artifact digests are spread into the same flat digest list as the complex and policy
    # digests, so this test is what fixes that layout. The multiplicity assertion matters
    # separately: a bundle over two copies of an upstream artifact is not the bundle over one.
    composed = cache_key(
        "feature-bundle/v1",
        digests=[_digest("1"), _digest("2"), _digest("3"), _digest("4")],
        fields={"feature_schema": "fs/v3", "model_input_contract": "mic/v2"},
    )

    assert (
        feature_bundle_key(
            _digest("1"), [_digest("2"), _digest("3")], "fs/v3", "mic/v2", _digest("4")
        )
        == composed
    )
    assert feature_bundle_key(
        _digest("1"), [_digest("2")], "fs/v3", "mic/v2", _digest("4")
    ) != feature_bundle_key(
        _digest("1"), [_digest("2"), _digest("2")], "fs/v3", "mic/v2", _digest("4")
    )


def test_feature_bundle_key_is_sensitive_to_every_input() -> None:
    variants = [
        (_digest("1"), [_digest("2")], "fs/v3", "mic/v2", _digest("4")),
        (_digest("5"), [_digest("2")], "fs/v3", "mic/v2", _digest("4")),
        (_digest("1"), [_digest("6")], "fs/v3", "mic/v2", _digest("4")),
        (_digest("1"), [_digest("2")], "fs/v4", "mic/v2", _digest("4")),
        (_digest("1"), [_digest("2")], "fs/v3", "mic/v3", _digest("4")),
        (_digest("1"), [_digest("2")], "fs/v3", "mic/v2", _digest("7")),
    ]
    keys = [feature_bundle_key(*arguments) for arguments in variants]

    assert len(set(keys)) == len(keys)


# --------------------------------------------------------------------------------------------
# store.py
# --------------------------------------------------------------------------------------------


def test_memory_store_reports_a_miss_as_none() -> None:
    assert MemoryStore().get(_digest("0")) is None


def test_memory_store_is_write_once_and_reports_the_stored_entry() -> None:
    # The entire value of a content-bound key is that what sits behind it never changes. If a
    # second write could replace the first, two callers holding the same key could be handed
    # different artifacts and a result already published would stop being reproducible.
    # `put_if_absent` therefore returns what is *stored* rather than what was offered, so a
    # racing writer learns it lost instead of assuming its own entry took effect.
    store = MemoryStore()
    first = _entry(producer_version="p1")
    contender = Entry(first.key, _artifact("b"), "p2", False)

    assert store.put_if_absent(first) is first
    assert store.put_if_absent(contender) is first
    assert store.get(first.key) is first


def test_memory_store_keeps_distinct_keys_independent() -> None:
    store = MemoryStore()
    left = Entry(_digest("1"), _artifact("a"), "p1", True)
    right = Entry(_digest("2"), _artifact("b"), "p2", False)

    store.put_if_absent(left)
    store.put_if_absent(right)

    assert store.get(left.key) is left
    assert store.get(right.key) is right


def test_entry_is_immutable() -> None:
    # An `Entry` is handed out to callers and to the policy. Mutating one in place would edit
    # the store's own record through an alias, which is the same write-once violation as a
    # second `put_if_absent` and harder to see.
    entry = _entry(qualified=False)

    with pytest.raises(FrozenInstanceError):
        entry.qualified = True


# --------------------------------------------------------------------------------------------
# policy.py
# --------------------------------------------------------------------------------------------


def test_default_policy_admits_only_qualified_entries() -> None:
    policy = CachePolicy()

    assert policy.accepts(_entry(qualified=True))
    assert not policy.accepts(_entry(qualified=False))


def test_qualification_can_be_waived_only_explicitly() -> None:
    # Serving unqualified output is sometimes right (a development loop, a re-derivation whose
    # result is about to be qualified) and is never a default.
    assert CachePolicy(require_qualified=False).accepts(_entry(qualified=False))


def test_producer_version_allowlist_excludes_unlisted_versions() -> None:
    # A producer version identifies the code that computed the value. Pinning it is how a
    # defect found in one producer is kept from being re-served out of the cache after the fix.
    policy = CachePolicy(accepted_producer_versions=frozenset({"p1", "p2"}))

    assert policy.accepts(_entry(producer_version="p1"))
    assert policy.accepts(_entry(producer_version="p2"))
    assert not policy.accepts(_entry(producer_version="p3"))


def test_an_empty_producer_version_allowlist_admits_every_version() -> None:
    # DOCUMENTED CURRENT BEHAVIOUR, and the sharpest edge in this package. The default
    # `accepted_producer_versions=frozenset()` reads as "nothing is accepted" and means "every
    # version is accepted" — the inverse of the fail-closed convention every other allowlist in
    # this tree follows. A caller that builds the set from configuration and gets an empty one
    # back (an unset variable, a renamed key) opts into every producer that ever wrote to the
    # cache rather than into none of them, and nothing reports it. Pinned so the behaviour is a
    # decision on record instead of a reading of the boolean expression.
    assert CachePolicy().accepted_producer_versions == frozenset()
    assert CachePolicy().accepts(_entry(producer_version="a-version-nobody-vetted"))


def test_policy_conditions_are_conjunctive() -> None:
    # Both gates have to hold. Satisfying one is what an entry from an allowed producer that
    # was never qualified looks like, and it must not be served.
    policy = CachePolicy(require_qualified=True, accepted_producer_versions=frozenset({"p1"}))

    assert policy.accepts(_entry(producer_version="p1", qualified=True))
    assert not policy.accepts(_entry(producer_version="p1", qualified=False))
    assert not policy.accepts(_entry(producer_version="p2", qualified=True))
    assert not policy.accepts(_entry(producer_version="p2", qualified=False))


# --------------------------------------------------------------------------------------------
# lookup.py
# --------------------------------------------------------------------------------------------


def test_lookup_reports_a_missing_key_as_none() -> None:
    assert lookup(MemoryStore(), _digest("0"), CachePolicy()) is None


def test_lookup_returns_an_entry_the_policy_accepts() -> None:
    store = MemoryStore()
    entry = _entry(qualified=True)
    store.put_if_absent(entry)

    assert lookup(store, entry.key, CachePolicy()) is entry


def test_a_rejected_hit_is_indistinguishable_from_a_miss() -> None:
    # `lookup` collapses "stored but not admissible" into the same `None` a miss returns, so
    # every caller has exactly one branch: recompute. Returning the entry with a flag, or
    # raising, would push the decision to serve an unqualified result back out to each call
    # site — which is where it gets made wrong. Asserted next to the raw `store.get` on
    # purpose: the point is that the entry *is* there and is still not served.
    store = MemoryStore()
    entry = _entry(qualified=False)
    store.put_if_absent(entry)

    assert store.get(entry.key) is entry
    assert lookup(store, entry.key, CachePolicy()) is None


def test_lookup_reads_through_the_store_protocol_and_never_writes() -> None:
    # Exercised against a store that is not `MemoryStore`, because the protocol is the seam
    # every real backend arrives through; a `lookup` coupled to the in-memory implementation
    # would pass every other test in this file.
    entry = _entry(qualified=True)
    store = _RecordingStore(entry)

    assert lookup(store, entry.key, CachePolicy()) is entry
    assert store.reads == [entry.key]


# --------------------------------------------------------------------------------------------
# promotion.py
# --------------------------------------------------------------------------------------------


def test_promote_qualifies_an_entry_and_changes_nothing_else() -> None:
    # Promotion is a statement about evidence, not about content. If it could alter the key,
    # the artifact or the producer version, it would be re-pointing a published key at
    # different bytes under the guise of a qualification decision.
    entry = _entry(qualified=False)
    promoted = promote(entry)

    assert promoted.qualified is True
    assert (promoted.key, promoted.artifact, promoted.producer_version) == (
        entry.key,
        entry.artifact,
        entry.producer_version,
    )


def test_promote_leaves_the_original_entry_untouched() -> None:
    entry = _entry(qualified=False)

    promote(entry)

    assert entry.qualified is False


def test_promote_is_idempotent() -> None:
    # Qualification can be re-run — the evidence pipeline is retried and replayed — so applying
    # it twice has to be the same value as applying it once.
    entry = _entry(qualified=False)
    already_qualified = _entry(qualified=True)

    assert promote(promote(entry)) == promote(entry)
    assert promote(already_qualified) == already_qualified


def test_a_promotion_cannot_be_persisted_through_the_store_protocol() -> None:
    # DOCUMENTED CURRENT BEHAVIOUR and a genuine gap. `promote` returns a new `Entry`, and the
    # only writer on `Store` is `put_if_absent`, which is first-write-wins — so offering the
    # promoted entry under its own key returns the unqualified original and leaves the store
    # exactly as it was. Qualifying an already-cached result therefore has nowhere to land, and
    # `lookup` under the default policy keeps rejecting it indefinitely. Recorded as a test
    # rather than repaired here: widening the protocol with an update path is a contract change
    # and does not belong in a change that only adds coverage.
    store = MemoryStore()
    entry = _entry(qualified=False)
    store.put_if_absent(entry)

    stored = store.put_if_absent(promote(entry))
    persisted = store.get(entry.key)

    assert stored is entry
    assert persisted is not None
    assert persisted.qualified is False
    assert lookup(store, entry.key, CachePolicy()) is None
