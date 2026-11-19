# Changelog

## [1.4.0](https://github.com/k0sind/k0sind/compare/v1.3.0...v1.4.0) (2026-11-19)


### Features

* add --kubeconfig to create, delete and export ([1574188](https://github.com/k0sind/k0sind/commit/157418838576fab75c230c79893d4d6342237cdf))
* apply KubeletConfiguration patches as the default worker profile ([a880f52](https://github.com/k0sind/k0sind/commit/a880f52cb9acdd53196baf13eb0c26f7b3407c73))
* honour KIND_EXPERIMENTAL_DOCKER_NETWORK ([1bd3271](https://github.com/k0sind/k0sind/commit/1bd32710a6f5acff35b9d8c887a5955784734cbe))
* keep the kubelet root at /var/lib/kubelet ([f652fbf](https://github.com/k0sind/k0sind/commit/f652fbfbb41ff25a15e9a56e7c2421450ffd85fe))
* make node mounts rshared so propagation workloads start ([d4e8170](https://github.com/k0sind/k0sind/commit/d4e81706187eb0efc8ddffe4b6d9a371300f1360))

## [1.3.0](https://github.com/k0sind/k0sind/compare/v1.2.0...v1.3.0) (2026-11-12)


### Features

* allow selecting the k0s release via K0S_VERSION ([f7fc98f](https://github.com/k0sind/k0sind/commit/f7fc98fcb32686dd7860c1d850e60184a5d58b86))
* make kind compabilible ([000aa64](https://github.com/k0sind/k0sind/commit/000aa64f879636b543fa0c522c48736770e1e04a))


### Bug Fixes

* add missing load command, drop unfinished storage step ([53465ac](https://github.com/k0sind/k0sind/commit/53465ac52836b9e8f5259d0bb5e6f5092f1b857d))
* **deps:** bump k8s.io/client-go from 0.36.3 to 0.37.2 ([5b83639](https://github.com/k0sind/k0sind/commit/5b836394dad2c1982993982af4ac4dcb86fadb04))
* **deps:** bump sigs.k8s.io/kind from 0.32.0 to 0.33.0 ([dcc3e7f](https://github.com/k0sind/k0sind/commit/dcc3e7f65ebe6af8a053c9bca961f52692c92f60))
* update k0s version in demo.svg ([a0f569e](https://github.com/k0sind/k0sind/commit/a0f569e0ff5bae85329a9efaa968585d7ccdcf76))
