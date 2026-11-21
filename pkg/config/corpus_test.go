package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"sigs.k8s.io/kind/pkg/apis/config/v1alpha4"
)

// The corpus in testdata/kind is kind's own set of config fixtures. k0sind
// promises to read kind configs, not to act on all of them, so each fixture
// states which of the three outcomes it must get: honored, warned about, or
// refused. The one outcome no input may get is being parsed into a cluster
// that quietly differs from what the file asked for.
type corpusCase struct {
	file     string
	wantErr  string   // substring of the expected error; empty means it must load
	nodes    int      // expected node count once defaults are applied
	warnings []string // substrings, one per expected warning; order-independent
}

var corpus = []corpusCase{{
	file:  "valid-minimal.yaml",
	nodes: 1, // no nodes listed: defaults to a single control-plane
}, {
	file:  "valid-minimal-two-nodes.yaml",
	nodes: 2,
}, {
	file:  "valid-port-and-mount.yaml",
	nodes: 1, // extraPortMappings and extraMounts are honored, so no warning
}, {
	file:     "valid-many-fields.yaml",
	nodes:    2,
	warnings: []string{"featureGates", "networking.ipFamily"},
}, {
	// kind builds HA clusters; k0sind cannot yet, and must say so rather than
	// silently dropping the extra controllers.
	file:    "valid-full-ha.yaml",
	wantErr: "single control-plane node",
}, {
	file:  "valid-kind-patches.yaml",
	nodes: 4,
	warnings: []string{
		"kubeadmConfigPatches (except KubeletConfiguration)",
		"containerdConfigPatches",
	},
}, {
	file:  "valid-kind-workers-patches.yaml",
	nodes: 4,
	warnings: []string{
		"nodes[1].kubeadmConfigPatches",
		"nodes[2].kubeadmConfigPatches",
	},
}, {
	file:    "invalid-yaml.yaml",
	wantErr: "parse kind config",
}, {
	file:    "invalid-bogus-field.yaml",
	wantErr: "parse kind config",
}, {
	file:    "invalid-bad-indent.yaml",
	wantErr: "parse kind config",
}, {
	// A kubeadm manifest or an unrelated YAML must not be read as an empty
	// cluster and silently turned into a default single-node one.
	file:    "invalid-kind.yaml",
	wantErr: `kind: want "Cluster"`,
}, {
	file:    "invalid-apiversion.yaml",
	wantErr: "apiVersion",
}}

func TestKindConfigCorpus(t *testing.T) {
	for _, tc := range corpus {
		t.Run(tc.file, func(t *testing.T) {
			c, warnings, err := Load(filepath.Join("testdata", "kind", tc.file))

			if tc.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got none", tc.wantErr)
				}
				if !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("error = %v, want it to contain %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			if len(c.Nodes) != tc.nodes {
				t.Errorf("got %d nodes, want %d", len(c.Nodes), tc.nodes)
			}
			assertWarnings(t, warnings, tc.warnings)
		})
	}
}

// assertWarnings checks that every expected warning was raised and that no
// field was ignored without one. An unexpected warning is as much a bug as a
// missing one: it means we are dropping something the user wrote.
func assertWarnings(t *testing.T, got, want []string) {
	t.Helper()
	matched := make([]bool, len(got))
	for _, w := range want {
		found := false
		for i, g := range got {
			if !matched[i] && strings.Contains(g, w) {
				matched[i], found = true, true
				break
			}
		}
		if !found {
			t.Errorf("missing warning about %q; got %v", w, got)
		}
	}
	for i, g := range got {
		if !matched[i] {
			t.Errorf("unexpected warning: %q", g)
		}
	}
}

// TestExampleConfigsLoad keeps the shipped examples in the contract: every
// kind-schema file under examples/ must load. e2e covers two of them, but it
// needs docker, so a broken example otherwise reaches CI late or not at all.
func TestExampleConfigsLoad(t *testing.T) {
	files, err := filepath.Glob(filepath.Join("..", "..", "examples", "*.yaml"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no examples found: %v", err)
	}
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("read %s: %v", f, err)
		}
		// The k0s.yaml examples are passed to --k0s-config, not --config.
		if !strings.Contains(string(data), GroupVersion) {
			continue
		}
		t.Run(filepath.Base(f), func(t *testing.T) {
			if _, _, err := Load(f); err != nil {
				t.Errorf("Load: %v", err)
			}
		})
	}
}

// Every field of kind's schema, classified. honored fields change what k0sind
// builds; warned fields are reported as ignored; inert fields carry no
// behavior to honor or drop.
//
// This exists so that bumping sigs.k8s.io/kind cannot add a field that k0sind
// reads straight past. A new field fails this test until someone decides which
// of the three it is — the decision is cheap, noticing it is not.
var (
	clusterFields = map[string]string{
		"Name":                            "honored",
		"Nodes":                           "honored",
		"Networking":                      "honored", // see networkingFields
		"FeatureGates":                    "warned",
		"RuntimeConfig":                   "warned",
		"KubeadmConfigPatches":            "honored", // KubeletConfiguration only, rest warned
		"KubeadmConfigPatchesJSON6902":    "warned",
		"ContainerdConfigPatches":         "warned",
		"ContainerdConfigPatchesJSON6902": "warned",
		"TypeMeta":                        "honored", // kind/apiVersion are validated
	}
	nodeFields = map[string]string{
		"Role":                         "honored",
		"Image":                        "honored",
		"ExtraMounts":                  "honored",
		"ExtraPortMappings":            "honored",
		"Labels":                       "warned",
		"KubeadmConfigPatches":         "warned",
		"KubeadmConfigPatchesJSON6902": "warned",
	}
	networkingFields = map[string]string{
		"APIServerAddress":  "honored",
		"APIServerPort":     "honored",
		"IPFamily":          "warned", // anything but ipv4
		"PodSubnet":         "warned",
		"ServiceSubnet":     "warned",
		"DisableDefaultCNI": "warned",
		"KubeProxyMode":     "warned",
		"DNSSearch":         "warned",
	}
)

func TestKindSchemaCoverage(t *testing.T) {
	for _, tc := range []struct {
		name    string
		typ     reflect.Type
		classes map[string]string
	}{
		{"Cluster", reflect.TypeOf(v1alpha4.Cluster{}), clusterFields},
		{"Node", reflect.TypeOf(v1alpha4.Node{}), nodeFields},
		{"Networking", reflect.TypeOf(v1alpha4.Networking{}), networkingFields},
	} {
		t.Run(tc.name, func(t *testing.T) {
			seen := map[string]bool{}
			for i := 0; i < tc.typ.NumField(); i++ {
				f := tc.typ.Field(i).Name
				seen[f] = true
				if _, ok := tc.classes[f]; !ok {
					t.Errorf("v1alpha4.%s.%s is new in the kind schema and is "+
						"neither honored nor warned about; classify it in %s",
						tc.name, f, "corpus_test.go")
				}
			}
			for f := range tc.classes {
				if !seen[f] {
					t.Errorf("v1alpha4.%s.%s no longer exists in the kind schema; "+
						"drop it from the classification", tc.name, f)
				}
			}
		})
	}
}

// TestWarnedFieldsActuallyWarn pins the other half of the promise: a field
// classified as warned must produce a warning when it is set. The
// classification above is a list of intentions; this checks they hold.
func TestWarnedFieldsActuallyWarn(t *testing.T) {
	for _, tc := range []struct {
		field  string
		mutate func(*v1alpha4.Cluster)
	}{
		{"FeatureGates", func(c *v1alpha4.Cluster) { c.FeatureGates = map[string]bool{"AllBeta": true} }},
		{"RuntimeConfig", func(c *v1alpha4.Cluster) { c.RuntimeConfig = map[string]string{"api/all": "true"} }},
		{"KubeadmConfigPatchesJSON6902", func(c *v1alpha4.Cluster) {
			c.KubeadmConfigPatchesJSON6902 = []v1alpha4.PatchJSON6902{{Kind: "ClusterConfiguration"}}
		}},
		{"ContainerdConfigPatches", func(c *v1alpha4.Cluster) { c.ContainerdConfigPatches = []string{"[plugins]"} }},
		{"ContainerdConfigPatchesJSON6902", func(c *v1alpha4.Cluster) {
			c.ContainerdConfigPatchesJSON6902 = []string{"[]"}
		}},
		{"Networking.DisableDefaultCNI", func(c *v1alpha4.Cluster) { c.Networking.DisableDefaultCNI = true }},
		{"Networking.KubeProxyMode", func(c *v1alpha4.Cluster) { c.Networking.KubeProxyMode = v1alpha4.IPVSProxyMode }},
		{"Networking.PodSubnet", func(c *v1alpha4.Cluster) { c.Networking.PodSubnet = "10.244.0.0/16" }},
		{"Networking.ServiceSubnet", func(c *v1alpha4.Cluster) { c.Networking.ServiceSubnet = "10.96.0.0/16" }},
		{"Networking.IPFamily", func(c *v1alpha4.Cluster) { c.Networking.IPFamily = v1alpha4.IPv6Family }},
		{"Networking.DNSSearch", func(c *v1alpha4.Cluster) { s := []string{"example.com"}; c.Networking.DNSSearch = &s }},
		{"Node.Labels", func(c *v1alpha4.Cluster) {
			c.Nodes = []v1alpha4.Node{{Role: v1alpha4.ControlPlaneRole, Labels: map[string]string{"a": "b"}}}
		}},
		{"Node.KubeadmConfigPatches", func(c *v1alpha4.Cluster) {
			c.Nodes = []v1alpha4.Node{{Role: v1alpha4.ControlPlaneRole, KubeadmConfigPatches: []string{"kind: JoinConfiguration"}}}
		}},
	} {
		t.Run(tc.field, func(t *testing.T) {
			c := &v1alpha4.Cluster{}
			tc.mutate(c)
			applyDefaults(c)
			if w := unsupportedWarnings(c); len(w) == 0 {
				t.Errorf("setting %s produced no warning; it would be ignored silently", tc.field)
			}
		})
	}
}
