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

## Installing From Release Binaries

Download the `k0sind` binary for your platform from the
[releases](https://github.com/josef-hak/k0sind/releases) page.

### Linux

~~~bash
VERSION=v1.1.0
# For AMD64 / x86_64
[ $(uname -m) = x86_64 ] && curl -Lo ./k0sind "https://github.com/josef-hak/k0sind/releases/download/${VERSION}/k0sind-linux-amd64"
# For ARM64
[ $(uname -m) = aarch64 ] && curl -Lo ./k0sind "https://github.com/josef-hak/k0sind/releases/download/${VERSION}/k0sind-linux-arm64"
chmod +x ./k0sind
sudo mv ./k0sind /usr/local/bin/k0sind
~~~

### macOS

~~~bash
VERSION=v1.1.0
# For Intel Macs
[ $(uname -m) = x86_64 ] && curl -Lo ./k0sind "https://github.com/josef-hak/k0sind/releases/download/${VERSION}/k0sind-darwin-amd64"
# For M1 / ARM Macs
[ $(uname -m) = arm64 ] && curl -Lo ./k0sind "https://github.com/josef-hak/k0sind/releases/download/${VERSION}/k0sind-darwin-arm64"
chmod +x ./k0sind
mv ./k0sind /some-dir-in-your-PATH/k0sind
~~~

### Windows (PowerShell)

~~~powershell
VERSION=v1.1.0
curl.exe -Lo k0sind-windows-amd64.exe "https://github.com/josef-hak/k0sind/releases/download/${VERSION}/k0sind-windows-amd64.exe"
Move-Item .\k0sind-windows-amd64.exe c:\some-dir-in-your-PATH\k0sind.exe
~~~

### From source

~~~bash
make install                       # into $GOBIN, version stamped in
# or
make build && ./bin/k0sind version
~~~

## Usage

~~~bash
# Single-node cluster (control-plane also schedules workloads)
k0sind create cluster --wait 120s

# From a kind-compatible config
k0sind create cluster -n dev --config examples/multi-node.yaml --wait 180s

# Point kubectl at it
kubectl config use-context k0sind-dev
kubectl get nodes

# Housekeeping
k0sind get clusters
k0sind get nodes -n dev
k0sind get kubeconfig -n dev
k0sind export kubeconfig -n dev
k0sind delete cluster -n dev
~~~

### Advanced usage
~~~bash
# Create k0s cluster with OpenEBS default Storage Class pre-installed
k0sind create cluster -n k0s-ebs --k0s-config examples/k0s-openebs.yaml

# Create k0s cluster with Calico network provider
k0sind create cluster -n k0s-calico --k0s-config examples/k0s-calico.yaml
~~~

### Flags (`create cluster`)

| Flag | Default | Description |
|------|---------|-------------|
| `-n` | `k0sind` | Cluster name (also the kubeconfig context `k0sind-<name>`) |
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

~~~bash
go build ./...
go test ./... -race            # unit tests, no docker
go test -tags e2e ./test/e2e/... -timeout 20m   # spins up real clusters
~~~

CI (`.github/workflows/ci.yml`) runs three jobs: `lint-build` (gofmt/vet/build),
`unit`, and `e2e` (a matrix of the single-node and 1cp+2w scenarios).

## Releasing

Releases are automated with [GoReleaser](https://goreleaser.com).

~~~bash
# test the full release build locally (no publishing) -> ./dist
make snapshot

# cut a real release: tag and push
git tag v0.1.0
git push origin v0.1.0
~~~

Pushing a `v*` tag triggers `.github/workflows/release.yml`, which vets, tests,
then builds the raw `k0sind-<os>-<arch>` binaries (Linux/macOS amd64+arm64,
Windows amd64) plus `checksums.txt` and attaches them to a GitHub Release. The
version is stamped into the binary via `-ldflags`, so `k0sind version` reports
the tag.
