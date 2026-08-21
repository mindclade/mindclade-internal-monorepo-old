"""Example patterns to adapt to a project's model and contract."""

from __future__ import annotations

import io

import pytest
import torch


class TinyModule(torch.nn.Module):
    def __init__(self) -> None:
        super().__init__()
        self.linear = torch.nn.Linear(4, 2)

    def forward(self, x: torch.Tensor) -> torch.Tensor:
        if x.ndim != 2 or x.shape[-1] != 4:
            raise ValueError("expected [batch, 4] input")
        return self.linear(x)


def test_forward_backward_contract() -> None:
    torch.manual_seed(0)
    model = TinyModule()
    x = torch.randn(3, 4)
    output = model(x)
    assert output.shape == (3, 2)
    assert torch.isfinite(output).all()

    loss = output.square().mean()
    loss.backward()
    gradients = [parameter.grad for parameter in model.parameters()]
    assert all(gradient is not None for gradient in gradients)
    assert all(torch.isfinite(gradient).all() for gradient in gradients if gradient is not None)


def test_state_dict_round_trip() -> None:
    torch.manual_seed(0)
    model = TinyModule().eval()
    x = torch.randn(3, 4)
    with torch.inference_mode():
        expected = model(x)

    buffer = io.BytesIO()
    torch.save(model.state_dict(), buffer)
    buffer.seek(0)

    restored = TinyModule().eval()
    state = torch.load(buffer, map_location="cpu", weights_only=True)
    restored.load_state_dict(state, strict=True)
    with torch.inference_mode():
        actual = restored(x)

    torch.testing.assert_close(actual, expected, rtol=1e-5, atol=1e-6)


@pytest.mark.skipif(not torch.cuda.is_available(), reason="CUDA is unavailable")
def test_cuda_forward_matches_cpu() -> None:
    torch.manual_seed(0)
    cpu_model = TinyModule().eval()
    cuda_model = TinyModule().eval().cuda()
    cuda_model.load_state_dict(cpu_model.state_dict())
    x = torch.randn(3, 4)

    with torch.inference_mode():
        cpu_output = cpu_model(x)
        cuda_output = cuda_model(x.cuda()).cpu()

    torch.testing.assert_close(cuda_output, cpu_output, rtol=1e-4, atol=1e-5)
