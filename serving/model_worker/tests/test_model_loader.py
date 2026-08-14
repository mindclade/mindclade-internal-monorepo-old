from serving.model_worker import LoadedModel, ModelRegistry

D = "sha256:" + "1" * 64


class Loader:
    def __init__(self) -> None:
        self.loads = 0

    def load(self, bundle_digest: str) -> LoadedModel:
        self.loads += 1
        return LoadedModel(bundle_digest, object())


def test_registry_loads_content_addressed_model_once() -> None:
    loader = Loader()
    registry = ModelRegistry(loader)
    assert registry.get_or_load(D) is registry.get_or_load(D)
    assert loader.loads == 1
