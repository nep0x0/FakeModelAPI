package provider

import (
	"fmt"
	"sort"
	"sync"
)

var (
	mu       sync.RWMutex
	registry = make(map[string]Provider)
)

// Register adds a provider to the registry.
func Register(name string, p Provider) {
	mu.Lock()
	defer mu.Unlock()
	registry[name] = p
}

// Get returns a provider by name.
func Get(name string) (Provider, error) {
	mu.RLock()
	defer mu.RUnlock()
	p, ok := registry[name]
	if !ok {
		return nil, fmt.Errorf("provider %q not found", name)
	}
	return p, nil
}

// List returns all registered provider names.
func List() []string {
	mu.RLock()
	defer mu.RUnlock()
	names := make([]string, 0, len(registry))
	for name := range registry {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// MustGet panics if the provider is not found (for cases where it should always exist).
func MustGet(name string) Provider {
	p, err := Get(name)
	if err != nil {
		panic(err)
	}
	return p
}
