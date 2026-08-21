# Export and deployment checklist

## Consumer contract

Record:

- target runtime and version;
- target hardware and precision;
- artifact format and opset or archive version;
- input names, nested structure, dtypes, and layout;
- static and dynamic dimensions with valid ranges;
- output names, order, dtypes, and semantics;
- preprocessing and postprocessing ownership;
- numerical tolerance and performance target.

## torch.export

`torch.export.export` captures a tensor computation graph from an `nn.Module` and example inputs. Unspecified dimensions are specialized by default. Use `torch.export.Dim`, `dynamic_shapes`, or supported helper APIs to describe intended dynamism, then test minimum, typical, and maximum shapes.

Save and load exported programs with the supported `torch.export.save` and `torch.export.load` APIs for the installed version. Keep the code and configuration required to reconstruct meaning around the artifact.

## ONNX

On current supported PyTorch releases, `torch.onnx.export(..., dynamo=True)` uses the `torch.export`-based exporter and is the recommended path. Verify the installed signature before adding version-specific options. With the dynamo path, prefer `dynamic_shapes`; legacy `dynamic_axes` belongs to older exporter behavior.

Run the resulting model in the actual ONNX consumer when possible. Export success alone does not prove operator, shape, precision, or performance compatibility.

## Control flow and custom operations

Python branches based on tensor data, mutation, unsupported objects, and custom operators can prevent sound graph capture. Use export-supported control-flow operators or a registered decomposition or translation only when semantics are understood and covered by parity tests.

## Parity matrix

Test:

- minimum, ordinary, and maximum dynamic shapes;
- batch size one;
- each supported dtype and precision;
- optional inputs and masks;
- numerically sensitive cases;
- output structure and names;
- a fresh-process load;
- the real target runtime.

## Artifact manifest

A useful manifest includes:

```text
artifact filename and SHA-256
model configuration or identifier
source commit or code version
PyTorch version and build
exporter and target-runtime versions
opset or archive version
input and output contract
dynamic shape constraints
precision and device target
validation inputs and tolerances
validation command and result
known unsupported cases
```

Official references:

- torch.export: https://docs.pytorch.org/docs/stable/user_guide/torch_compiler/export.html
- torch.export API: https://docs.pytorch.org/docs/stable/user_guide/torch_compiler/export/api_reference.html
- ONNX export: https://docs.pytorch.org/docs/stable/onnx.html
- Serialization security: https://docs.pytorch.org/docs/stable/notes/serialization.html
