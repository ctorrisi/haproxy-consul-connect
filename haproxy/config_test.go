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
	maxconn 512
	nbthread 1
	stats timeout 2m
	tune.bufsize 8192
	tune.maxrewrite 1024
	tune.ssl.cachesize 100
	tune.ssl.default-dh-param 2048
	ulimit-n 4096
	with.dots hey.I.have.dots
	with.spaces hey I have spaces

defaults
	another abdc
	http-reuse always
	multiple key1 value1
	multiple key2 value2
	test.with.dots 3

`
	require.Equal(t, expected_conf, capture_stdout.String())
}
