# Kubernetes

There is no chart in this repository. The one on Artifact Hub is [upstream's](https://artifacthub.io/packages/helm/makeplane/plane-ce), and installed as it comes it deploys **upstream's** Plane: `makeplane/plane-backend` is Django and `makeplane/plane-live` is Node, while everything in this tree is Go.

The chart itself is fine — the images are the only difference. Point it at this fork's namespace:

```
helm repo add plane-ce https://helm.plane.so/
helm install plane plane-ce/plane-ce \
  --set dockerhub_user=yldm-tech
```

Check the value's name against the chart version you install (`helm show values plane-ce/plane-ce`); it is the one every image reference is built from.

What that gets you is the Go API, worker, beat, migrator and collaborative editor, with the three frontends and the proxy unchanged.

The two deployments this repository does define are [`../../cli/community`](../../cli/community) for Compose and [`../../aio/community`](../../aio/community) for the single-container image.
