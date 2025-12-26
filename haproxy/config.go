package haproxy

import (
	"crypto/rand"
	"encoding/base64"
	"os"
	"path"
	"text/template"

	"github.com/haproxytech/haproxy-consul-connect/lib"
	"github.com/haproxytech/haproxy-consul-connect/utils"
	log "github.com/sirupsen/logrus"
)

var defaultsHAProxyParams = utils.HAProxyParams{
	Globals: map[string][]string{
		"stats":           {"timeout 2m"},
		"nbthread":        {"1"},
		"ulimit-n":        {"4096"},
		"maxconn":         {"1024"},
		"tune.bufsize":    {"8192"},
		"tune.maxrewrite": {"1024"},
	},
	Defaults: map[string][]string{
		"http-reuse": {"always"},
	},
}

const baseCfgTmpl = `
global
	stats socket {{.SocketPath}} mode 600 level admin expose-fd listeners
	expose-experimental-directives
	{{- range $k, $vs := .HAProxyParams.Globals}}
	{{- range $v := $vs}}
	{{$k}} {{$v}}
	{{- end }}
	{{- end }}

defaults
	{{- range $k, $vs := .HAProxyParams.Defaults}}
	{{- range $v := $vs}}
	{{$k}} {{$v}}
	{{- end }}
	{{- end }}
	compression algo gzip
	compression type text/css text/html text/javascript application/javascript text/plain text/xml application/json

`

const spoeConfTmpl = `
[intentions]

spoe-agent intentions-agent
	messages check-intentions

	option var-prefix connect

	timeout hello      3000ms
	timeout idle       3000s
	timeout processing 3000ms

	use-backend spoe_back

spoe-message check-intentions
	args ip=src cert=ssl_c_der
	event on-frontend-tcp-request

`

type baseParams struct {
	SocketPath    string
	HAProxyParams utils.HAProxyParams
}

type haConfig struct {
	Base             string
	HAProxy          string
	SPOE             string
	SPOESock         string
	StatsSock        string
	MasterSocketPath string
	LogsSock         string
}

func newHaConfig(baseDir string, params utils.HAProxyParams, sd *lib.Shutdown) (*haConfig, error) {
	cfg := &haConfig{}

	sd.Add(1)
	base, err := os.MkdirTemp(baseDir, "haproxy-connect-")
	if err != nil {
		sd.Done()
		return nil, err
	}
	go func() {
		defer sd.Done()
		<-sd.Stop
		log.Info("cleaning config...")
		os.RemoveAll(base)
	}()

	cfg.Base = base

	cfg.HAProxy = path.Join(base, "haproxy.conf")
	cfg.SPOE = path.Join(base, "spoe.conf")
	cfg.SPOESock = path.Join(base, "spoe.sock")
	cfg.StatsSock = path.Join(base, "haproxy.sock")
	cfg.MasterSocketPath = path.Join(base, "haproxy-master.sock")
	cfg.LogsSock = path.Join(base, "logs.sock")

	tmpl, err := template.New("cfg").Parse(baseCfgTmpl)
	if err != nil {
		return nil, err
	}

	cfgFile, err := os.OpenFile(cfg.HAProxy, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
	if err != nil {
		return nil, err
	}
	defer func() {
		err := cfgFile.Close()
		if err != nil {
			log.Errorf("error closing config file %s: %s", cfg.HAProxy, err)
		}
	}()

	err = tmpl.Execute(cfgFile, baseParams{
		SocketPath:    cfg.StatsSock,
		HAProxyParams: defaultsHAProxyParams.With(params),
	})
	if err != nil {
		sd.Done()
		return nil, err
	}

	spoeCfgFile, err := os.OpenFile(cfg.SPOE, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
	if err != nil {
		sd.Done()
		return nil, err
	}
	defer func() {
		err := spoeCfgFile.Close()
		if err != nil {
			log.Errorf("error closing spoe config file %s: %s", cfg.SPOE, err)
		}
	}()
	_, err = spoeCfgFile.WriteString(spoeConfTmpl)
	if err != nil {
		sd.Done()
		return nil, err
	}

	return cfg, nil
}

func createRandomString() string {
	randBytes := make([]byte, 32)
	_, _ = rand.Read(randBytes)
	return base64.URLEncoding.EncodeToString(randBytes)
}
