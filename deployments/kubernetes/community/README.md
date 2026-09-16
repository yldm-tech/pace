# Kubernetes

There is no chart in this repository. The one on Artifact Hub is [upstream's](https://artifacthub.io/packages/helm/makeplane/plane-ce), and installed as it comes it deploys **upstream's** Plane: `makeplane/plane-backend` is Django and `makeplane/plane-live` is Node, while everything in this tree is Go.

The chart itself is fine — workloads, services, ingress and dependencies are all reusable, and the images are the only difference. [`values.yaml`](values.yaml) beside this file overrides exactly those:

```
helm repo add plane-ce https://helm.plane.so/
helm install plane plane-ce/plane-ce -f values.yaml
```

What that gets you is the Go API, worker, beat, migrator and collaborative editor, with the three frontends unchanged.

Two things to check before installing:

- **`planeVersion`.** It is the tag applied to every image, so it has to name a release this repository has published — see [releases](https://github.com/yldm-tech/pace/releases) or the packages listed on the repository's ghcr namespace. The chart's own default names an upstream version, which does not exist here.
- **The value names.** They were verified against `plane-ce` 1.8.1 (`web.image`, `space.image`, `admin.image`, `live.image`, `api.image`, `worker.image`, `beatworker.image`). A later chart may rename them; `helm show values plane-ce/plane-ce` is the authority for the version you install.

There is no `proxy` override because the chart has no proxy workload — it routes through an ingress instead, so `plane-proxy` has no counterpart in a Kubernetes install.

The two deployments this repository does define are [`../../cli/community`](../../cli/community) for Compose and [`../../aio/community`](../../aio/community) for the single-container image.
