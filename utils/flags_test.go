package utils

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMakeHAProxyParams(t *testing.T) {
	flags := StringSliceFlag{
		"defaults.test.with.dots=3",
		"defaults.another=abdc",
		"defaults.another=efgh",
		"global.with.spaces=hey I have spaces",
		"global.with.dots=hey.I.have.dots",
	}

	r, err := MakeHAProxyParams(flags)
	require.NoError(t, err)

	// MakeHAProxyParams now includes defaults, so we expect both defaults and user-provided params
	require.Equal(t, HAProxyParams{
		Defaults: map[string][]string{
			"http-reuse":              {"always"}, // from defaults
			"timeout connect":         {"5s"},
			"timeout client":          {"30s"},
			"timeout server":          {"30s"},
			"timeout http-request":    {"5s"},
			"timeout http-keep-alive": {"15s"},
			"timeout queue":           {"5s"},
			"retries":                 {"3"},
			"option clitcpka":         {""},
			"option srvtcpka":         {""},
			"option redispatch":       {""},
			"test.with.dots":          {"3"},
			"another":                 {"abdc", "efgh"},
		},
		Globals: map[string][]string{
			"stats":                     {"timeout 2m"}, // from defaults
			"nbthread":                  {"2"},
			"ulimit-n":                  {"4096"},
			"maxconn":                   {"1024"},
			"tune.bufsize":              {"16384"},
			"tune.maxrewrite":           {"1024"},
			"tune.ssl.cachesize":        {"500"},
			"tune.ssl.default-dh-param": {"2048"},
			"with.spaces":               {"hey I have spaces"},
			"with.dots":                 {"hey.I.have.dots"},
		},
	}, r)
}
