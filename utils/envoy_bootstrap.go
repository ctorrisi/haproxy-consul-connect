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
	DynamicResources struct {
		AdsConfig struct {
			GrpcServices []struct {
				EnvoyGrpc struct {
					ClusterName string `json:"cluster_name"`
				} `json:"envoy_grpc"`
				InitialMetadata []struct {
					Key   string `json:"key"`
					Value string `json:"value"`
				} `json:"initial_metadata"`
			} `json:"grpc_services"`
		} `json:"ads_config"`
	} `json:"dynamic_resources"`
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

	var config EnvoyBootstrapConfig
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse envoy bootstrap JSON: %w", err)
	}

	return &config, nil
}

// ExtractConsulToken extracts the Consul token from the bootstrap config
func (c *EnvoyBootstrapConfig) ExtractConsulToken() string {
	if c == nil {
		return ""
	}

	for _, grpcService := range c.DynamicResources.AdsConfig.GrpcServices {
		for _, metadata := range grpcService.InitialMetadata {
			if metadata.Key == "x-consul-token" {
				return metadata.Value
			}
		}
	}

	return ""
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
