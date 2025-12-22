package utils

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseEnvoyBootstrap(t *testing.T) {
	// Create a temporary bootstrap file
	tmpDir := t.TempDir()
	bootstrapPath := filepath.Join(tmpDir, "envoy_bootstrap.json")

	bootstrapJSON := `{
  "node": {
    "cluster": "backend-service",
    "id": "_nomad-task-abc123-group-web-backend-service-sidecar-proxy"
  },
  "dynamic_resources": {
    "ads_config": {
      "grpc_services": {
        "initial_metadata": [{
          "key": "x-consul-token",
          "value": "test-token-12345"
        }],
        "envoy_grpc": {
          "cluster_name": "local_agent"
        }
      }
    }
  }
}`

	err := os.WriteFile(bootstrapPath, []byte(bootstrapJSON), 0644)
	require.NoError(t, err)

	// Test parsing
	config, err := ParseEnvoyBootstrap(bootstrapPath)
	require.NoError(t, err)
	require.NotNil(t, config)

	// Test token extraction
	token := config.ExtractConsulToken()
	require.Equal(t, "test-token-12345", token)

	// Test service name extraction
	serviceName := config.ExtractServiceName()
	require.Equal(t, "service", serviceName)
}

func TestParseEnvoyBootstrap_NoFile(t *testing.T) {
	config, err := ParseEnvoyBootstrap("/does/not/exist")
	require.NoError(t, err)
	require.Nil(t, config)
}

func TestParseEnvoyBootstrap_EmptyPath(t *testing.T) {
	config, err := ParseEnvoyBootstrap("")
	require.NoError(t, err)
	require.Nil(t, config)
}

func TestExtractServiceName_Various(t *testing.T) {
	tests := []struct {
		name     string
		proxyID  string
		cluster  string
		expected string
	}{
		{
			name:     "nomad format",
			proxyID:  "_nomad-task-abc-group-web-backend-service-sidecar-proxy",
			expected: "service",
		},
		{
			name:     "simple format",
			proxyID:  "my-service-sidecar-proxy",
			expected: "service",
		},
		{
			name:     "fallback to cluster",
			proxyID:  "some-other-id",
			cluster:  "my-cluster",
			expected: "my-cluster",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := &EnvoyBootstrapConfig{}
			config.Node.ID = tt.proxyID
			config.Node.Cluster = tt.cluster

			result := config.ExtractServiceName()
			require.Equal(t, tt.expected, result)
		})
	}
}
