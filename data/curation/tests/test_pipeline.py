from data.curation.pipeline import CuratedRecord, CurationPipeline, identity


def test_pipeline_is_deterministic_and_sorts_by_key() -> None:
    records = [
        CuratedRecord("b", b"two"),
        CuratedRecord("a", b"one"),
    ]
    pipeline = CurationPipeline([identity])
    first = pipeline.run(records)
    second = pipeline.run(reversed(records))
    assert [record.key for record in first.records] == ["a", "b"]
    assert first.manifest_digest == second.manifest_digest


def test_pipeline_counts_drops() -> None:
    pipeline = CurationPipeline([lambda record: None if record.key == "drop" else record])
    result = pipeline.run([CuratedRecord("keep", b"x"), CuratedRecord("drop", b"y")])
    assert result.input_records == 2
    assert result.dropped_records == 1
    assert [record.key for record in result.records] == ["keep"]


def test_pipeline_rejects_conflicting_duplicate_keys() -> None:
    pipeline = CurationPipeline([identity])
    try:
        pipeline.run([CuratedRecord("same", b"a"), CuratedRecord("same", b"b")])
    except ValueError as error:
        assert "conflicting curated records" in str(error)
    else:
        raise AssertionError("expected duplicate conflict")
