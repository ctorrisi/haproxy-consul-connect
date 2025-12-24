package renderer

import (
	"bytes"
	"fmt"
	"text/template"

	"github.com/haproxytech/haproxy-consul-connect/haproxy/state"
)

type Renderer struct{}

func New() *Renderer {
	return &Renderer{}
}

type renderContext struct {
	SocketPath    string
	HAProxyParams HAProxyParams
	Frontends     []state.Frontend
	Backends      []state.Backend
}

type HAProxyParams struct {
	Globals  map[string][]string
	Defaults map[string][]string
}

const configTemplate = `global
	stats socket {{.SocketPath}} mode 600 level admin expose-fd listeners
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

{{range .Frontends}}
frontend {{.Frontend.Name}}
	{{- if .Frontend.Mode}}
	mode {{.Frontend.Mode}}
	{{- end}}
	{{- if .Bind.Address}}
	bind {{.Bind.Address}}:{{derefInt64 .Bind.Port}}{{if .Bind.Ssl}} ssl crt {{.Bind.SslCertificate}}{{if .Bind.SslCafile}} ca-file {{.Bind.SslCafile}}{{end}}{{if .Bind.Verify}} verify {{.Bind.Verify}}{{end}}{{if .Bind.NoVerifyhost}} no-verifyhost{{end}}{{end}}
	{{- end}}
	{{- if .Frontend.DefaultBackend}}
	default_backend {{.Frontend.DefaultBackend}}
	{{- end}}
	{{- if .Frontend.ClientTimeout}}
	timeout client {{.Frontend.ClientTimeout}}ms
	{{- end}}
	{{- if .Frontend.Httplog}}
	option httplog
	{{- end}}
	{{- if .FilterSpoe}}
	filter spoe engine {{.FilterSpoe.Filter.SpoeEngine}} config {{.FilterSpoe.Filter.SpoeConfig}}
	tcp-request content {{.FilterSpoe.Rule.Action}}{{if .FilterSpoe.Rule.Cond}} {{.FilterSpoe.Rule.Cond}}{{end}}{{if .FilterSpoe.Rule.CondTest}} {{.FilterSpoe.Rule.CondTest}}{{end}}
	{{- end}}
	{{- if .FilterCompression}}
	filter compression
	{{- end}}
	{{- if .LogTarget}}
	log {{.LogTarget.Address}} {{.LogTarget.Facility}}{{if .LogTarget.Format}} {{.LogTarget.Format}}{{end}}
	{{- end}}
{{end}}

{{range .Backends}}
backend {{.Backend.Name}}
	{{- if .Backend.Mode}}
	mode {{.Backend.Mode}}
	{{- end}}
	{{- if .Backend.Balance}}
	{{- if .Backend.Balance.Algorithm}}
	balance {{.Backend.Balance.Algorithm}}
	{{- end}}
	{{- end}}
	{{- if .Backend.ServerTimeout}}
	timeout server {{.Backend.ServerTimeout}}ms
	{{- end}}
	{{- if .Backend.ConnectTimeout}}
	timeout connect {{.Backend.ConnectTimeout}}ms
	{{- end}}
	{{- if .Backend.Forwardfor}}
	{{- if .Backend.Forwardfor.Enabled}}
	{{- if eq .Backend.Forwardfor.Enabled "enabled"}}
	option forwardfor
	{{- end}}
	{{- end}}
	{{- end}}
	{{- if .LogTarget}}
	log {{.LogTarget.Address}} {{.LogTarget.Facility}}{{if .LogTarget.Format}} {{.LogTarget.Format}}{{end}}
	{{- end}}
	{{- range .HTTPRequestRules}}
	http-request {{.Type}}{{if .HdrName}} {{.HdrName}}{{end}}{{if .HdrFormat}} {{.HdrFormat}}{{end}}
	{{- end}}
	{{- range .Servers}}
	server {{.Name}} {{.Address}}:{{derefInt64 .Port}}{{if .Ssl}} ssl crt {{.SslCertificate}}{{if .SslCafile}} ca-file {{.SslCafile}}{{end}}{{if .Verify}} verify {{.Verify}}{{end}}{{if .NoVerifyhost}} no-verifyhost{{end}}{{end}}{{if .Weight}} weight {{derefInt64 .Weight}}{{end}}{{if eq .Maintenance "enabled"}} disabled{{end}}{{if eq .Check "enabled"}} check{{end}}
	{{- end}}
{{end}}
`

func (r *Renderer) Render(st state.State, socketPath string, haproxyParams HAProxyParams) (string, error) {
	funcMap := template.FuncMap{
		"derefInt64": func(p *int64) int64 {
			if p == nil {
				return 0
			}
			return *p
		},
	}

	tmpl, err := template.New("config").Funcs(funcMap).Parse(configTemplate)
	if err != nil {
		return "", fmt.Errorf("failed to parse template: %w", err)
	}

	ctx := renderContext{
		SocketPath:    socketPath,
		HAProxyParams: haproxyParams,
		Frontends:     st.Frontends,
		Backends:      st.Backends,
	}

	var buf bytes.Buffer
	err = tmpl.Execute(&buf, ctx)
	if err != nil {
		return "", fmt.Errorf("failed to execute template: %w", err)
	}

	return buf.String(), nil
}
