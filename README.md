# k0sind — k0s IN Docker

`k0sind` is like [kind](https://kind.sigs.k8s.io/), but each node runs
[k0s](https://k0sproject.io/) instead of kubeadm-based Kubernetes. It reads
**kind-compatible** config files (`kind.x-k8s.io/v1alpha4`), so if you already
use kind you can point k0sind at the same YAML and get a k0s cluster in Docker.

Because k0s ships as a single self-contained binary with a built-in CNI, k0sind
is small: it is essentially a translator from a kind config into
`docker run` / `k0s token create` / `k0s worker <token>` calls.

## Requirements

- Docker (daemon running, reachable via the `docker` CLI)
- Go 1.26+ (only to build from source)

## Install

```sh
go install github.com/k0sproject/k0sind@latest
```

## Usage

```sh
# Single-node cluster (control-plane also schedules workloads)
k0sind create cluster --wait 120s

# From a kind-compatible config
k0sind create cluster --name dev --config examples/multi-node.yaml --wait 180s

# Point kubectl at it
kubectl config use-context k0sind-dev
kubectl get nodes

# Housekeeping
k0sind get clusters
k0sind get nodes --name dev
k0sind export kubeconfig --name dev
k0sind delete cluster --name dev
```

### Flags (`create cluster`)

| Flag | Default | Description |
|------|---------|-------------|
| `--name` | `k0sind` | Cluster name (also the kubeconfig context `k0sind-<name>`) |
| `--config` | – | Path to a kind-compatible config file |
| `--image` | pinned k0s release | k0s node image override |
| `--wait` | `0` | Wait for all nodes to be `Ready` (e.g. `120s`); `0` returns once started |

## kind compatibility

k0sind is a **migration layer**, not a drop-in for every kind feature. It honors
the parts of the kind schema that map to k0s and warns about the rest.

| Supported | Ignored (with a warning) |
|-----------|--------------------------|
| `name`, `nodes[].role`, `nodes[].image` | `kubeadmConfigPatches*` |
| `nodes[].extraPortMappings` | `containerdConfigPatches*` |
| `nodes[].extraMounts` | `networking.disableDefaultCNI`, `kubeProxyMode` |
| `networking.apiServerAddress`, `apiServerPort` | `podSubnet`, `serviceSubnet`, `featureGates`, `runtimeConfig` |

These are ignored because k0s does not use kubeadm and manages CNI/networking
through its own `k0s.yaml`. Mapping them onto k0s config is future work.

## Topology (v0.1.0)

- **1 control-plane + N workers.** The control-plane runs
  `k0s controller --enable-worker`; workers join with a token
  (`k0s token create --role=worker`).
- A single-node cluster additionally passes `--no-taints` so pods schedule on it.
- Multi-controller HA is planned for a later release.

## Development

```sh
go build ./...
go test ./... -race            # unit tests, no docker
go test -tags e2e ./test/e2e/... -timeout 20m   # spins up real clusters
```

CI (`.github/workflows/ci.yml`) runs three jobs: `lint-build` (gofmt/vet/build),
`unit`, and `e2e` (a matrix of the single-node and 1cp+2w scenarios).
