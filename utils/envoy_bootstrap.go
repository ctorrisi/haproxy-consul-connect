package utils

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	log "github.com/sirupsen/logrus"
)

// EnvoyBootstrapConfig represents the relevant parts of Envoy's bootstrap configuration
type EnvoyBootstrapConfig struct {
	Node struct {
		ID      string `json:"id"`
		Cluster string `json:"cluster"`
	} `json:"node"`
	// Use RawMessage to handle flexible JSON structure
	DynamicResources json.RawMessage `json:"dynamic_resources"`
	// Store extracted values
	consulToken string
}

// ParseEnvoyBootstrap reads and parses an Envoy bootstrap file
func ParseEnvoyBootstrap(path string) (*EnvoyBootstrapConfig, error) {
	if path == "" {
		return nil, nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			log.Debugf("Envoy bootstrap file not found at %s", path)
			return nil, nil
		}
		return nil, fmt.Errorf("failed to read envoy bootstrap file: %w", err)
	}

	log.Infof("Envoy bootstrap data %s", string(data))

	var config EnvoyBootstrapConfig
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse envoy bootstrap JSON: %w", err)
	}

	// Extract the Consul token from the flexible JSON structure
	config.consulToken = extractTokenFromJSON(config.DynamicResources)

	if config.consulToken != "" {
		log.Debug("Extracted Consul token from Envoy bootstrap")
	}

	return &config, nil
}

// extractTokenFromJSON searches for x-consul-token in the dynamic_resources JSON
// Expected structure: dynamic_resources.ads_config.grpc_services.initial_metadata[].{key,value}
func extractTokenFromJSON(rawJSON json.RawMessage) string {
	var data map[string]interface{}
	if err := json.Unmarshal(rawJSON, &data); err != nil {
		return ""
	}

	// Navigate to ads_config.grpc_services.initial_metadata
	adsConfig, ok := data["ads_config"].(map[string]interface{})
	if !ok {
		return ""
	}

	grpcServices, ok := adsConfig["grpc_services"].(map[string]interface{})
	if !ok {
		return ""
	}

	initialMetadata, ok := grpcServices["initial_metadata"].([]interface{})
	if !ok {
		return ""
	}

	// Look for x-consul-token in the metadata array
	for _, item := range initialMetadata {
		metadata, ok := item.(map[string]interface{})
		if !ok {
			continue
		}

		key, keyOk := metadata["key"].(string)
		value, valueOk := metadata["value"].(string)

		if keyOk && valueOk && key == "x-consul-token" {
			return value
		}
	}

	return ""
}

// ExtractConsulToken extracts the Consul token from the bootstrap config
func (c *EnvoyBootstrapConfig) ExtractConsulToken() string {
	if c == nil {
		return ""
	}
	return c.consulToken
}

// ExtractServiceName extracts the service name from the proxy ID
// Proxy ID format: _nomad-task-XXXXX-group-GROUPNAME-SERVICENAME-sidecar-proxy
func (c *EnvoyBootstrapConfig) ExtractServiceName() string {
	if c == nil {
		return ""
	}

	proxyID := c.Node.ID
	if proxyID == "" {
		return ""
	}

	// For Nomad-generated IDs, the service name is in the proxy ID
	// Try to extract it by looking for the pattern
	if strings.Contains(proxyID, "-sidecar-proxy") {
		parts := strings.Split(proxyID, "-")
		// Find the index of "sidecar"
		for i, part := range parts {
			if part == "sidecar" && i > 0 {
				// The service name is the part before "sidecar"
				return parts[i-1]
			}
		}
	}

	// If we can't parse it, return the cluster name as fallback
	if c.Node.Cluster != "" {
		return c.Node.Cluster
	}

	return ""
}
