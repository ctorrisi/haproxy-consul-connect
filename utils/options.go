package utils

type HAProxyParams struct {
	Defaults map[string][]string
	Globals  map[string][]string
}

// DefaultHAProxyParams provides optimized defaults for HAProxy in service mesh sidecars
var DefaultHAProxyParams = HAProxyParams{
	Globals: map[string][]string{
		"stats":                     {"timeout 2m"},
		"nbthread":                  {"1"}, // Single thread is efficient for most sidecars
		"ulimit-n":                  {"4096"},
		"maxconn":                   {"512"},   // Adequate for typical sidecar workloads
		"tune.bufsize":              {"16384"}, // 16 KB - better for service mesh payloads (still 64x smaller than Envoy's 1MB)
		"tune.maxrewrite":           {"1024"},
		"tune.ssl.cachesize":        {"500"},  // Optimized for repeated connections to mesh peers
		"tune.ssl.default-dh-param": {"2048"}, // Explicitly set (prevents larger default)
	},
	Defaults: map[string][]string{
		"http-reuse": {"always"},
	},
}

func (p HAProxyParams) With(other HAProxyParams) HAProxyParams {
	new := HAProxyParams{
		Defaults: map[string][]string{},
		Globals:  map[string][]string{},
	}
	for k, v := range p.Defaults {
		new.Defaults[k] = v
	}
	for k, v := range other.Defaults {
		new.Defaults[k] = v
	}
	for k, v := range p.Globals {
		new.Globals[k] = v
	}
	for k, v := range other.Globals {
		new.Globals[k] = v
	}
	return new
}

type Options struct {
	HAProxyBin           string
	ConfigBaseDir        string
	SPOEAddress          string
	EnableIntentions     bool
	StatsListenAddr      string
	StatsRegisterService bool
	LogRequests          bool
	HAProxyParams        HAProxyParams
}
