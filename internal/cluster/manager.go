package cluster

import (
	"fmt"
	"sync"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/clientcmd/api"
)

// Manager manages multi-cluster kubeconfig contexts and lazy clientsets.
type Manager struct {
	mu         sync.Mutex
	rawConfig  api.Config
	active     string
	clientsets map[string]*kubernetes.Clientset
}

// New loads the default kubeconfig and returns a Manager.
func New() (*Manager, error) {
	rules := clientcmd.NewDefaultClientConfigLoadingRules()
	cfg := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(rules, &clientcmd.ConfigOverrides{})
	raw, err := cfg.RawConfig()
	if err != nil {
		return nil, fmt.Errorf("load kubeconfig: %w", err)
	}
	active := raw.CurrentContext
	if active == "" && len(raw.Contexts) > 0 {
		for k := range raw.Contexts {
			active = k
			break
		}
	}
	return &Manager{
		rawConfig:  raw,
		active:     active,
		clientsets: make(map[string]*kubernetes.Clientset),
	}, nil
}

// Contexts returns all context names sorted alphabetically.
func (m *Manager) Contexts() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]string, 0, len(m.rawConfig.Contexts))
	for k := range m.rawConfig.Contexts {
		out = append(out, k)
	}
	return out
}

// ActiveContext returns the currently active context name.
func (m *Manager) ActiveContext() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.active
}

// ActiveNamespace returns the namespace configured for the active context.
func (m *Manager) ActiveNamespace() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if ctx, ok := m.rawConfig.Contexts[m.active]; ok && ctx.Namespace != "" {
		return ctx.Namespace
	}
	return "default"
}

// SwitchContext switches to the named context and clears the cached clientset.
func (m *Manager) SwitchContext(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.rawConfig.Contexts[name]; !ok {
		return fmt.Errorf("context %q not found in kubeconfig", name)
	}
	m.active = name
	return nil
}

// ActiveClientset returns (or lazily creates) a clientset for the active context.
func (m *Manager) ActiveClientset() (*kubernetes.Clientset, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.clientsetFor(m.active)
}

// ActiveRestConfig returns the REST config for the active context.
// Needed by attach and port-forward which use client-go SPDY directly.
func (m *Manager) ActiveRestConfig() (*rest.Config, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	overrides := &clientcmd.ConfigOverrides{CurrentContext: m.active}
	rules := clientcmd.NewDefaultClientConfigLoadingRules()
	cfg := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(rules, overrides)
	restCfg, err := cfg.ClientConfig()
	if err != nil {
		return nil, fmt.Errorf("rest config for %q: %w", m.active, err)
	}
	restCfg.WarningHandler = rest.NoWarnings{}
	return restCfg, nil
}

// ClientsetFor returns a clientset for the named context.
func (m *Manager) ClientsetFor(ctx string) (*kubernetes.Clientset, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.clientsetFor(ctx)
}

func (m *Manager) clientsetFor(ctx string) (*kubernetes.Clientset, error) {
	if cs, ok := m.clientsets[ctx]; ok {
		return cs, nil
	}
	overrides := &clientcmd.ConfigOverrides{CurrentContext: ctx}
	rules := clientcmd.NewDefaultClientConfigLoadingRules()
	cfg := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(rules, overrides)
	restCfg, err := cfg.ClientConfig()
	if err != nil {
		return nil, fmt.Errorf("rest config for %q: %w", ctx, err)
	}
	restCfg.WarningHandler = rest.NoWarnings{}
	cs, err := kubernetes.NewForConfig(restCfg)
	if err != nil {
		return nil, fmt.Errorf("clientset for %q: %w", ctx, err)
	}
	m.clientsets[ctx] = cs
	return cs, nil
}
