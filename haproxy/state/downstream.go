package state

import (
	"fmt"

	"github.com/haproxytech/haproxy-consul-connect/consul"
	"github.com/haproxytech/models/v2"
	log "github.com/sirupsen/logrus"
)

func generateDownstream(opts Options, certStore CertificateStore, cfg consul.Downstream, state State) (State, error) {
	feName := "front_downstream"
	beName := "back_downstream"
	feMode := models.FrontendModeHTTP
	beMode := models.BackendModeHTTP

	caPath, crtPath, err := certStore.CertsPath(cfg.TLS)
	if err != nil {
		return state, err
	}

	if cfg.Protocol == "tcp" {
		feMode = models.FrontendModeTCP
		beMode = models.BackendModeTCP
	}

	log.Infof("downstream: configuring frontend to listen on %s:%d, backend target %s:%d",
		cfg.LocalBindAddress, cfg.LocalBindPort, cfg.TargetAddress, cfg.TargetPort)

	// Main config
	fe := Frontend{
		Frontend: models.Frontend{
			Name:           feName,
			DefaultBackend: beName,
			ClientTimeout:  int64p(int(cfg.ReadTimeout.Milliseconds())),
			Mode:           feMode,
			Httplog:        opts.LogRequests,
		},
		Bind: models.Bind{
			Name:           fmt.Sprintf("%s_bind", feName),
			Address:        cfg.LocalBindAddress,
			Port:           int64p(cfg.LocalBindPort),
			Ssl:            true,
			SslCertificate: crtPath,
			SslCafile:      caPath,
			Verify:         models.BindVerifyNone,
		},
		FilterCompression: &FrontendFilter{
			Filter: models.Filter{
				Type: models.FilterTypeCompression,
			},
		},
	}

	// Logging
	if opts.LogRequests && opts.LogSocket != "" {
		fe.LogTarget = &models.LogTarget{
			Address:  opts.LogSocket,
			Facility: models.LogTargetFacilityLocal0,
			Format:   models.LogTargetFormatRfc5424,
		}
	}

	// Intentions
	if opts.EnableIntentions {
		fe.FilterSpoe = &FrontendFilter{
			Filter: models.Filter{
				Type:       models.FilterTypeSpoe,
				SpoeEngine: "intentions",
				SpoeConfig: opts.SPOEConfigPath,
			},
			Rule: models.TCPRequestRule{
				Action:   models.TCPRequestRuleActionReject,
				Cond:     models.TCPRequestRuleCondUnless,
				CondTest: "{ var(sess.connect.auth) -m int eq 1 }",
				Type:     models.TCPRequestRuleTypeContent,
			},
		}
	}

	state.Frontends = append(state.Frontends, fe)

	var forwardFor *models.Forwardfor
	if cfg.EnableForwardFor && beMode == models.BackendModeHTTP {
		forwardFor = &models.Forwardfor{
			Enabled: stringp(models.ForwardforEnabledEnabled),
		}
	}

	// Backend
	be := Backend{
		Backend: models.Backend{
			Name:           beName,
			ServerTimeout:  int64p(int(cfg.ReadTimeout.Milliseconds())),
			ConnectTimeout: int64p(int(cfg.ConnectTimeout.Milliseconds())),
			Mode:           beMode,
			Forwardfor:     forwardFor,
			Balance: &models.Balance{
				Algorithm: stringp(models.BalanceAlgorithmRoundrobin),
			},
		},
		Servers: []models.Server{
			{
				Name:    "downstream_node",
				Address: cfg.TargetAddress,
				Port:    int64p(cfg.TargetPort),
				// Circuit breaker pattern for downstream health
				// - Infrequent checks in steady state (300s)
				// - Fast reaction to state changes (2s)
				// - Immediate failover on connection errors
				Check:       models.ServerCheckEnabled,
				Inter:       int64p(300000), // 300s normal interval
				Fastinter:   int64p(2000),   // 2s when transitioning UP
				Downinter:   int64p(2000),   // 2s when transitioning DOWN
				Rise:        int64p(1),      // 1 success = UP
				Fall:        int64p(1),      // 1 failure = DOWN
				Observe:     models.ServerObserveLayer4,
				ErrorLimit:  1,                            // Trip after 1 error
				OnError:     models.ServerOnErrorMarkDown, // Immediate failover
				Maintenance: models.ServerMaintenanceDisabled,
			},
		},
	}

	// Logging
	if opts.LogRequests && opts.LogSocket != "" {
		be.LogTarget = &models.LogTarget{
			Address:  opts.LogSocket,
			Facility: models.LogTargetFacilityLocal0,
			Format:   models.LogTargetFormatRfc5424,
		}
	}

	// App name header
	if cfg.AppNameHeaderName != "" && beMode == models.BackendModeHTTP {
		be.HTTPRequestRules = append(be.HTTPRequestRules, models.HTTPRequestRule{
			Type:      models.HTTPRequestRuleTypeAddHeader,
			HdrName:   cfg.AppNameHeaderName,
			HdrFormat: "%[var(sess.connect.source_app)]",
		})
	}

	state.Backends = append(state.Backends, be)

	return state, nil
}
