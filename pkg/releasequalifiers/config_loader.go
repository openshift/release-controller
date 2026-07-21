package releasequalifiers

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/fsnotify/fsnotify"
	"gopkg.in/yaml.v3"
	"k8s.io/klog/v2"
)

// ConfigAccessor provides thread-safe access to release qualifiers configuration
type ConfigAccessor interface {
	Get() ReleaseQualifiers
}

// ConfigLoader loads and watches release qualifiers configuration file
type ConfigLoader struct {
	mu         sync.RWMutex
	config     ReleaseQualifiers
	configPath string
	watcher    *fsnotify.Watcher
	onChange   []func(ReleaseQualifiers)
}

// NewConfigLoader creates a new ConfigLoader
// If configPath is empty or file doesn't exist, starts with empty config and logs warning
func NewConfigLoader(configPath string) (*ConfigLoader, error) {
	cl := &ConfigLoader{
		configPath: configPath,
		config:     make(ReleaseQualifiers),
		onChange:   make([]func(ReleaseQualifiers), 0),
	}

	// If configPath is empty, start with empty config
	if configPath == "" {
		klog.V(2).Info("Release qualifiers config path is empty, starting with empty config")
		return cl, nil
	}

	// Try to load initial config
	if err := cl.loadConfig(); err != nil {
		// Log warning but don't fail - graceful degradation
		klog.Warningf("Failed to load release qualifiers config from %s: %v (starting with empty config)", configPath, err)
	}

	return cl, nil
}

// Get returns the current configuration (thread-safe)
func (cl *ConfigLoader) Get() ReleaseQualifiers {
	cl.mu.RLock()
	defer cl.mu.RUnlock()

	// Return a deep copy to prevent external modification of pointer-backed fields
	result := make(ReleaseQualifiers, len(cl.config))
	for k, v := range cl.config {
		result[k] = *v.DeepCopy()
	}
	return result
}

// StartWatching begins watching the config file for changes
// Supports both regular files and Kubernetes ConfigMap mounts (..data symlink)
func (cl *ConfigLoader) StartWatching(ctx context.Context) error {
	if cl.configPath == "" {
		klog.V(2).Info("Config path is empty, skipping file watching")
		return nil
	}

	// Create fsnotify watcher
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("failed to create fsnotify watcher: %w", err)
	}
	cl.watcher = watcher

	// Watch the parent directory of the config file. For ConfigMap mounts this is
	// the mount root where the ..data symlink lives, so we receive CREATE events
	// when Kubernetes atomically swaps it during ConfigMap updates.
	dir := filepath.Dir(cl.configPath)

	if err := watcher.Add(dir); err != nil {
		watcher.Close()
		return fmt.Errorf("failed to watch directory %s: %w", dir, err)
	}

	klog.V(2).Infof("Started watching %s for release qualifiers config changes", dir)

	go cl.watch(ctx)

	return nil
}

// Subscribe adds a callback to be notified when config changes
func (cl *ConfigLoader) Subscribe(callback func(ReleaseQualifiers)) {
	cl.mu.Lock()
	defer cl.mu.Unlock()
	cl.onChange = append(cl.onChange, callback)
}

// watch monitors file system events and reloads config when changes are detected
func (cl *ConfigLoader) watch(ctx context.Context) {
	defer cl.watcher.Close()

	for {
		select {
		case <-ctx.Done():
			klog.V(2).Info("Stopping release qualifiers config file watcher")
			return

		case event, ok := <-cl.watcher.Events:
			if !ok {
				klog.Warning("Watcher events channel closed")
				return
			}

			// Handle both regular file writes and ConfigMap updates
			// ConfigMap updates: CREATE event when ..data symlink is recreated
			// Regular files: WRITE event
			shouldReload := false

			if event.Op&fsnotify.Write == fsnotify.Write && event.Name == cl.configPath {
				// Direct write to config file
				shouldReload = true
			} else if event.Op&fsnotify.Create == fsnotify.Create && filepath.Base(event.Name) == "..data" {
				shouldReload = true
			}

			if shouldReload {
				klog.V(2).Infof("Detected change to release qualifiers config: %s", event.Name)

				// Reload config
				if err := cl.loadConfig(); err != nil {
					klog.Errorf("Failed to reload release qualifiers config: %v (keeping previous config)", err)
					continue
				}

				klog.Info("Successfully reloaded release qualifiers config")

				// Notify subscribers
				cl.notifySubscribers()
			}

		case err, ok := <-cl.watcher.Errors:
			if !ok {
				klog.Warning("Watcher errors channel closed")
				return
			}
			klog.Errorf("Watcher error: %v", err)
		}
	}
}

// loadConfig loads configuration from file
// Returns error if file exists but is invalid
// Returns nil (empty config) if file doesn't exist
func (cl *ConfigLoader) loadConfig() error {
	// Check if file exists
	if _, err := os.Stat(cl.configPath); err != nil {
		if os.IsNotExist(err) {
			klog.Warningf("Release qualifiers config file not found: %s, using empty config", cl.configPath)
			cl.mu.Lock()
			cl.config = make(ReleaseQualifiers)
			cl.mu.Unlock()
			return nil
		}
		return fmt.Errorf("failed to stat config file: %w", err)
	}

	// Read file
	data, err := os.ReadFile(cl.configPath)
	if err != nil {
		return fmt.Errorf("failed to read config file: %w", err)
	}

	// Parse YAML using strict decoder
	var configFile ConfigFile
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true) // Strict decoding - reject unknown fields

	if err := decoder.Decode(&configFile); err != nil {
		return fmt.Errorf("failed to decode YAML: %w", err)
	}

	config := configFile.Qualifiers
	if config == nil {
		config = make(ReleaseQualifiers)
	}

	// Update config (thread-safe)
	cl.mu.Lock()
	cl.config = config
	cl.mu.Unlock()

	klog.V(2).Infof("Loaded %d release qualifiers from %s", len(config), cl.configPath)
	return nil
}

// notifySubscribers notifies all registered callbacks of config changes
func (cl *ConfigLoader) notifySubscribers() {
	cl.mu.RLock()
	callbacks := make([]func(ReleaseQualifiers), len(cl.onChange))
	copy(callbacks, cl.onChange)
	config := cl.Get() // Get a copy of current config
	cl.mu.RUnlock()

	for _, callback := range callbacks {
		callback(config)
	}
}
