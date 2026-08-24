// Package kubeconfig exports a k0s admin kubeconfig into the user's ~/.kube/config
// as a dedicated k0sind context, rewriting the API server address to the
// host-mapped port. It mirrors how kind manages kubeconfig entries.
package kubeconfig

import (
	"fmt"
	"os"
	"path/filepath"

	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

// ContextName returns the kubeconfig context name for a cluster (e.g. k0sind-dev).
func ContextName(clusterName string) string {
	return "k0sind-" + clusterName
}

// Path returns the kubeconfig file to write to: the first entry of $KUBECONFIG
// if set, otherwise ~/.kube/config.
func Path() (string, error) {
	if env := os.Getenv("KUBECONFIG"); env != "" {
		return filepath.SplitList(env)[0], nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".kube", "config"), nil
}

// Merge rewrites the raw admin kubeconfig to point at host:port and merges it
// into the user's kubeconfig under the k0sind-<name> context, selecting it.
func Merge(clusterName string, rawAdmin []byte, host, port string) error {
	incoming, err := rewrite(clusterName, rawAdmin, host, port)
	if err != nil {
		return err
	}
	path, err := Path()
	if err != nil {
		return err
	}
	existing := loadOrNew(path)

	key := ContextName(clusterName)
	for k, v := range incoming.Clusters {
		existing.Clusters[k] = v
	}
	for k, v := range incoming.AuthInfos {
		existing.AuthInfos[k] = v
	}
	for k, v := range incoming.Contexts {
		existing.Contexts[k] = v
	}
	existing.CurrentContext = key

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return clientcmd.WriteToFile(*existing, path)
}

// Remove deletes a cluster's context/cluster/user entries from the user's
// kubeconfig. Missing entries are ignored.
func Remove(clusterName string) error {
	path, err := Path()
	if err != nil {
		return err
	}
	cfg, err := clientcmd.LoadFromFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	key := ContextName(clusterName)
	delete(cfg.Clusters, key)
	delete(cfg.AuthInfos, key)
	delete(cfg.Contexts, key)
	if cfg.CurrentContext == key {
		cfg.CurrentContext = ""
	}
	return clientcmd.WriteToFile(*cfg, path)
}

// Rewrite normalizes the raw k0s admin kubeconfig, renaming entries to
// k0sind-<name> and pointing the server at host:port. It returns the
// serialized kubeconfig YAML.
func Rewrite(clusterName string, rawAdmin []byte, host, port string) ([]byte, error) {
	cfg, err := rewrite(clusterName, rawAdmin, host, port)
	if err != nil {
		return nil, err
	}
	return clientcmd.Write(*cfg)
}

// rewrite normalizes the single-entry k0s admin kubeconfig: it renames the
// cluster/user/context to the k0sind key and points the server at host:port.
// When host is empty the original server address is preserved (internal mode).
func rewrite(clusterName string, raw []byte, host, port string) (*clientcmdapi.Config, error) {
	cfg, err := clientcmd.Load(raw)
	if err != nil {
		return nil, fmt.Errorf("parse admin kubeconfig: %w", err)
	}
	key := ContextName(clusterName)

	out := clientcmdapi.NewConfig()
	for _, cl := range cfg.Clusters {
		if host != "" {
			cl.Server = fmt.Sprintf("https://%s:%s", host, port)
		}
		out.Clusters[key] = cl
		break
	}
	for _, a := range cfg.AuthInfos {
		out.AuthInfos[key] = a
		break
	}
	if len(out.Clusters) == 0 || len(out.AuthInfos) == 0 {
		return nil, fmt.Errorf("admin kubeconfig missing cluster or user entry")
	}
	out.Contexts[key] = &clientcmdapi.Context{Cluster: key, AuthInfo: key}
	out.CurrentContext = key
	return out, nil
}

// loadOrNew loads the kubeconfig at path, or returns an empty config if it does
// not exist or cannot be parsed.
func loadOrNew(path string) *clientcmdapi.Config {
	cfg, err := clientcmd.LoadFromFile(path)
	if err != nil || cfg == nil {
		return clientcmdapi.NewConfig()
	}
	// Guard against nil maps from a partially-populated file.
	if cfg.Clusters == nil {
		cfg.Clusters = map[string]*clientcmdapi.Cluster{}
	}
	if cfg.AuthInfos == nil {
		cfg.AuthInfos = map[string]*clientcmdapi.AuthInfo{}
	}
	if cfg.Contexts == nil {
		cfg.Contexts = map[string]*clientcmdapi.Context{}
	}
	return cfg
}
