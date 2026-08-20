# Locked cert-manager static release

cert-manager is deployed from the upstream v1.19.1 static release, not from a Helm chart. The
checked-in generated artifacts are the only deployment source: `v1.19.1/crds` contains the six
CRDs, while `v1.19.1/controllers` contains the remaining upstream objects plus the reviewed HA,
resource, system-node, image-digest, and namespace-ownership overlay.

`split_release.py` accepts no URL and performs no download. A dependency promotion must fetch the
URL in `infra/gitops/argocd/bootstrap/argocd.lock.yaml`, verify its byte count and SHA-256, then run
the splitter with those exact locked values. The tool refuses a partial/mismatched input and proves
that the raw phase inventories are disjoint and their normalized union equals all 49 upstream
objects. The complete upstream file is temporary promotion input and must not be committed as a
third copy.

The CRD overlay adds `Prune=false,Delete=false`. The controller overlay omits the upstream
Namespace because `operator-system/foundation` owns it, pins all controller images by digest, and
adds two replicas, bounded resources, system-node placement, and one-minimum-available PDBs. It
does not create repository-authored Secrets; cert-manager creates its runtime serving certificate
through its controller protocol.
