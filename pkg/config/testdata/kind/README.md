# kind config fixtures

Copied verbatim from kind v0.32.0,
`pkg/internal/apis/config/encoding/testdata/`, which is Apache-2.0 licensed
(Copyright The Kubernetes Authors).

They are the corpus `TestKindConfigCorpus` runs through k0sind's loader. The
point is not that k0sind behaves like kind on every one of them — it cannot,
having no kubeadm — but that for each input it does exactly one of three
things: honors the field, warns that it is ignored, or refuses the config with
a clear error. Silently parsing a kind config into something different is the
failure this guards against.

Refresh them when the `sigs.k8s.io/kind` dependency is bumped; new fields in
the schema are caught separately by `TestKindSchemaCoverage`.
