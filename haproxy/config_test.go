package haproxy

import (
	"bytes"
	"fmt"
	"runtime"
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

	tmpl, err := template.New("test").Parse(baseCfgTmpl)
	var capture_stdout bytes.Buffer
	err = tmpl.Execute(&capture_stdout, baseParams{
		SocketPath:    "stats_sock.sock",
		DataplaneUser: "dummy_user",
		DataplanePass: "dummy_pass",
		HAProxyParams: defaultsHAProxyParams.With(params),
	})
	require.NoError(t, err)
	expected_conf := `
global
	stats socket stats_sock.sock mode 600 level admin expose-fd listeners
	maxconn 32000
	nbthread ` + fmt.Sprint(runtime.GOMAXPROCS(0)) + `
	stats timeout 2m
	ulimit-n 65536
	with.dots hey.I.have.dots
	with.spaces hey I have spaces

defaults
	another abdc
	http-reuse always
	multiple key1 value1
	multiple key2 value2
	test.with.dots 3
	compression algo gzip
	compression type text/css text/html text/javascript application/javascript text/plain text/xml application/json

userlist controller
	user dummy_user insecure-password dummy_pass

`
	require.Equal(t, expected_conf, capture_stdout.String())
}
