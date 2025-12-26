package utils

type HAProxyParams struct {
	Defaults map[string][]string
	Globals  map[string][]string
}

// DefaultHAProxyParams provides optimized defaults for HAProxy in service mesh sidecars
var DefaultHAProxyParams = HAProxyParams{
	Globals: map[string][]string{
		"stats":                     {"timeout 2m"},
		"nbthread":                  {"2"},     // Matches Envoy default (concurrency: 2) for better multi-core utilization
		"ulimit-n":                  {"4096"},
		"maxconn":                   {"1024"},  // Matches Envoy default soft limit
		"tune.bufsize":              {"16384"}, // 16 KB - better for service mesh payloads (still 64x smaller than Envoy's 1MB)
		"tune.maxrewrite":           {"1024"},
		"tune.ssl.cachesize":        {"500"},   // Optimized for repeated connections to mesh peers
		"tune.ssl.default-dh-param": {"2048"},  // Explicitly set (prevents larger default)
	},
	Defaults: map[string][]string{
		// Connection pooling - critical for service mesh performance
		"http-reuse": {"always"},

		// Timeouts - prevent hung connections (matches Envoy defaults)
		"timeout connect":         {"5s"},  // Max time to establish backend connection (Envoy default)
		"timeout client":          {"30s"}, // Max client inactivity (Envoy default)
		"timeout server":          {"30s"}, // Max server inactivity (Envoy default)
		"timeout http-request":    {"5s"},  // Max time for complete HTTP request
		"timeout http-keep-alive": {"15s"}, // Max time waiting for new request on keep-alive (Envoy idle timeout)
		"timeout queue":           {"5s"},  // Max time request can stay in queue

		// Retries - improve resilience against transient failures
		"retries": {"3"}, // Retry failed connections up to 3 times

		// TCP keep-alive - detect dead connections (no memory overhead)
		"option clitcpka": {""}, // Enable TCP keep-alive on client side
		"option srvtcpka": {""}, // Enable TCP keep-alive on server side

		// Redispatch - try different servers on retry (service mesh resilience)
		"option redispatch": {""}, // Allow retries to different servers

		// Error responses - plain text instead of HTML (service mesh friendly)
		"http-error status 400 content-type text/plain lf-string": {`"Bad Request"`},
		"http-error status 403 content-type text/plain lf-string": {`"Forbidden"`},
		"http-error status 408 content-type text/plain lf-string": {`"Request Timeout"`},
		"http-error status 500 content-type text/plain lf-string": {`"Internal Server Error"`},
		"http-error status 502 content-type text/plain lf-string": {`"Bad Gateway"`},
		"http-error status 503 content-type text/plain lf-string": {`"Service Unavailable"`},
		"http-error status 504 content-type text/plain lf-string": {`"Gateway Timeout"`},
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
