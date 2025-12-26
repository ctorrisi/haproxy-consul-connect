package haproxy

import (
	"bytes"
	"testing"

	"github.com/haproxytech/haproxy-consul-connect/utils"
	"github.com/stretchr/testify/require"
	"text/template"
)

func TestHaproxyConfig(t *testing.T) {
	//	flags := stringSliceFlag{
	flags := []string{
		"defaults.test.with.dots=3",
		"defaults.another=abdc",
		"defaults.multiple key1=value1",
		"defaults.multiple key2=value2",
		"global.with.spaces=hey I have spaces",
		"global.with.dots=hey.I.have.dots",
	}

	params, err := utils.MakeHAProxyParams(flags)
	require.NoError(t, err)

	tmpl, err := template.New("test").Parse(baseCfgTmpl)
	require.NoError(t, err)

	var capture_stdout bytes.Buffer
	err = tmpl.Execute(&capture_stdout, baseParams{
		SocketPath:    "stats_sock.sock",
		HAProxyParams: params,
	})
	require.NoError(t, err)
	expected_conf := `
global
	stats socket stats_sock.sock mode 600 level admin expose-fd listeners
	expose-experimental-directives
	maxconn 1024
	nbthread 2
	stats timeout 2m
	tune.bufsize 16384
	tune.maxrewrite 1024
	tune.ssl.cachesize 500
	tune.ssl.default-dh-param 2048
	ulimit-n 4096
	with.dots hey.I.have.dots
	with.spaces hey I have spaces

defaults
	another abdc
	http-reuse always
	multiple key1 value1
	multiple key2 value2
	option clitcpka 
	option redispatch 
	option srvtcpka 
	retries 3
	test.with.dots 3
	timeout client 30s
	timeout connect 5s
	timeout http-keep-alive 15s
	timeout http-request 5s
	timeout queue 5s
	timeout server 30s

`
	require.Equal(t, expected_conf, capture_stdout.String())
}
