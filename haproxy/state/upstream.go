package state

import (
	"fmt"

	"github.com/haproxytech/haproxy-consul-connect/consul"
	"github.com/haproxytech/models/v2"
	log "github.com/sirupsen/logrus"
)

func generateUpstream(opts Options, certStore CertificateStore, cfg consul.Upstream, oldState, newState State) (State, error) {
	feName := fmt.Sprintf("front_%s", cfg.Name)
	beName := fmt.Sprintf("back_%s", cfg.Name)
	feMode := models.FrontendModeTCP
	beMode := models.BackendModeTCP

	fePort64 := int64(cfg.LocalBindPort)

	if cfg.Protocol == "http" {
		feMode = models.FrontendModeHTTP
		beMode = models.BackendModeHTTP
	}

	log.Infof("upstream %s: configuring frontend to listen on %s:%d", cfg.Name, cfg.LocalBindAddress, cfg.LocalBindPort)

	fe := Frontend{
		Frontend: models.Frontend{
			Name:           feName,
			DefaultBackend: beName,
			ClientTimeout:  int64p(int(cfg.ReadTimeout.Milliseconds())),
			Mode:           feMode,
			Httplog:        opts.LogRequests,
		},
		Bind: models.Bind{
			Name:    fmt.Sprintf("%s_bind", feName),
			Address: cfg.LocalBindAddress,
			Port:    &fePort64,
		},
	}

	// HTTP-specific features (disabled in TCP mode)
	if feMode == models.FrontendModeHTTP {
		fe.FilterCompression = &FrontendFilter{
			Filter: models.Filter{
				Type: models.FilterTypeCompression,
			},
		}
	}
	if opts.LogRequests && opts.LogSocket != "" {
		fe.LogTarget = &models.LogTarget{
			Address:  opts.LogSocket,
			Facility: models.LogTargetFacilityLocal0,
			Format:   models.LogTargetFormatRfc5424,
		}
	}

	newState.Frontends = append(newState.Frontends, fe)

	be := Backend{
		Backend: models.Backend{
			Name:           beName,
			ServerTimeout:  int64p(int(cfg.ReadTimeout.Milliseconds())),
			ConnectTimeout: int64p(int(cfg.ConnectTimeout.Milliseconds())),
			Balance: &models.Balance{
				Algorithm: stringp(models.BalanceAlgorithmLeastconn),
			},
			Mode: beMode,
		},
	}
	if opts.LogRequests && opts.LogSocket != "" {
		be.LogTarget = &models.LogTarget{
			Address:  opts.LogSocket,
			Facility: models.LogTargetFacilityLocal0,
			Format:   models.LogTargetFormatRfc5424,
		}
	}

	servers, err := generateUpstreamServers(opts, certStore, cfg, beName, oldState)
	if err != nil {
		return newState, err
	}
	be.Servers = servers

	// Dynamic retries: n-1 where n = number of servers (minimum 1)
	retries := int64(len(servers) - 1)
	if retries < 1 {
		retries = 1
	}
	be.Backend.Retries = &retries

	newState.Backends = append(newState.Backends, be)

	return newState, nil
}

func generateUpstreamServers(opts Options, certStore CertificateStore, cfg consul.Upstream, beName string, oldState State) ([]models.Server, error) {
	caPath, crtPath, err := certStore.CertsPath(cfg.TLS)
	if err != nil {
		return nil, err
	}

	servers := make([]models.Server, 0, len(cfg.Nodes))

	for i, node := range cfg.Nodes {
		log.Infof("upstream %s: configuring server %s:%d (weight: %d)", beName, node.Host, node.Port, node.Weight)

		server := models.Server{
			Name:           fmt.Sprintf("srv_%d", i),
			Address:        node.Host,
			Port:           int64p(node.Port),
			Weight:         int64p(node.Weight),
			Ssl:            models.ServerSslEnabled,
			SslCertificate: crtPath,
			SslCafile:      caPath,
			Verify:         models.ServerVerifyNone,
			Maintenance:    models.ServerMaintenanceDisabled,

			// Circuit breaker pattern for upstream health
			// Consul already health checks, but we add circuit breaker for fast failover
			Check:      models.ServerCheckEnabled,
			Inter:      int64p(300000), // 300s normal interval
			Fastinter:  int64p(2000),   // 2s when transitioning UP
			Downinter:  int64p(2000),   // 2s when transitioning DOWN
			Rise:       int64p(1),      // 1 success = UP
			Fall:       int64p(1),      // 1 failure = DOWN
			Observe:    models.ServerObserveLayer4,
			ErrorLimit: 1,                            // Trip after 1 error
			OnError:    models.ServerOnErrorMarkDown, // Immediate failover
		}

		servers = append(servers, server)
	}

	return servers, nil
}
