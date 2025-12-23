package consul

import (
	"crypto/x509"
	"fmt"
	"reflect"
	"sort"
	"sync"
	"time"

	"github.com/hashicorp/consul/api"
	log "github.com/sirupsen/logrus"
)

const (
	DefaultDownstreamBindAddr = "0.0.0.0"
	DefaultUpstreamBindAddr   = "127.0.0.1"
	DefaultReadTimeout        = 60 * time.Second
	DefaultConnectTimeout     = 30 * time.Second

	errorWaitTime             = 5 * time.Second
	preparedQueryPollInterval = 30 * time.Second
)

type upstream struct {
	LocalBindAddress string
	LocalBindPort    int
	Name             string
	Datacenter       string
	Protocol         string
	Nodes            []*api.ServiceEntry
	ReadTimeout      time.Duration
	ConnectTimeout   time.Duration

	done bool
}

type downstream struct {
	LocalBindAddress  string
	LocalBindPort     int
	Protocol          string
	TargetAddress     string
	TargetPort        int
	EnableForwardFor  bool
	AppNameHeaderName string
	ReadTimeout       time.Duration
	ConnectTimeout    time.Duration
}

type certLeaf struct {
	Cert []byte
	Key  []byte

	done bool
}

type Watcher struct {
	service     string
	serviceName string
	consul      *api.Client
	token       string
	C           chan Config

	lock  sync.Mutex
	ready sync.WaitGroup

	upstreams  map[string]*upstream
	downstream downstream
	certCAs    [][]byte
	certCAPool *x509.CertPool
	leaf       *certLeaf

	update chan struct{}
	log    Logger
}

// New builds a new watcher
func New(service string, consul *api.Client, log Logger) *Watcher {
	return &Watcher{
		service: service,
		consul:  consul,

		C:         make(chan Config),
		upstreams: make(map[string]*upstream),
		update:    make(chan struct{}, 1),
		log:       log,
	}
}

// findSidecarProxy queries Consul's agent API to find the sidecar proxy service
// for the given service. This mimics Envoy's approach of directly checking Consul's
// service catalog rather than using internal Nomad APIs.
func (w *Watcher) findSidecarProxy() (string, error) {
	// Query all services registered with this Consul agent
	services, err := w.consul.Agent().Services()
	if err != nil {
		return "", fmt.Errorf("failed to query Consul services: %w", err)
	}

	// Look for a sidecar proxy service for our service
	// The sidecar will have a name like "backend-service-sidecar-proxy"
	// and its metadata or tags should indicate it's a proxy for our service
	expectedProxyName := w.service + "-sidecar-proxy"

	for serviceID, service := range services {
		// Check if this is a sidecar proxy service
		if service.Kind == api.ServiceKindConnectProxy {
			// Check if the proxy field indicates this is for our service
			if service.Proxy != nil && service.Proxy.DestinationServiceName == w.service {
				w.log.Debugf("consul: found sidecar proxy %s (ID: %s) for service %s",
					service.Service, serviceID, w.service)
				return serviceID, nil
			}
		}

		// Also check by name pattern for compatibility
		if service.Service == expectedProxyName || serviceID == expectedProxyName {
			w.log.Debugf("consul: found sidecar proxy by name %s for service %s",
				serviceID, w.service)
			return serviceID, nil
		}
	}

	return "", fmt.Errorf("no sidecar proxy registered for %s", w.service)
}

func (w *Watcher) Run() error {
	// Retry lookup to handle race conditions (e.g., in Nomad where service starts before sidecar is registered)
	// Instead of using proxy.LookupServiceForSidecar (which is for Nomad internals),
	// we directly query Consul's agent API to find the registered sidecar proxy service
	var proxyID string
	var err error
	maxRetries := 60 // Increased to match Envoy's 60 second timeout
	retryDelay := 1 * time.Second

	for i := 0; i < maxRetries; i++ {
		proxyID, err = w.findSidecarProxy()
		if err == nil {
			break
		}

		if i < maxRetries-1 {
			w.log.Infof("consul: sidecar proxy not found for %s (attempt %d/%d), retrying in %s: %s", w.service, i+1, maxRetries, retryDelay, err)
			time.Sleep(retryDelay)
			// Exponential backoff with cap at 5 seconds
			if retryDelay < 5*time.Second {
				retryDelay = retryDelay * 2
			}
		}
	}

	if err != nil {
		return fmt.Errorf("failed to find sidecar proxy in Consul after %d attempts: %w", maxRetries, err)
	}

	w.log.Infof("consul: found sidecar proxy %s for service %s", proxyID, w.service)

	// Try to get the application service, but if it doesn't exist (common in Nomad),
	// fall back to using the service name we were given
	svc, _, err := w.consul.Agent().Service(w.service, &api.QueryOptions{})
	if err != nil {
		w.log.Warnf("consul: application service %s not found in Consul (this is normal in Nomad), using service name as-is: %v", w.service, err)
		w.serviceName = w.service
	} else {
		w.serviceName = svc.Service
	}

	// Get the sidecar proxy service details to extract the target port
	proxySvc, _, err := w.consul.Agent().Service(proxyID, &api.QueryOptions{})
	if err != nil {
		return fmt.Errorf("failed to get sidecar proxy details: %w", err)
	}

	// In Nomad, the application service may not be registered separately.
	// Get the target port from the proxy's LocalServicePort configuration.
	if proxySvc.Proxy != nil && proxySvc.Proxy.LocalServicePort > 0 {
		w.downstream.TargetPort = proxySvc.Proxy.LocalServicePort
		w.log.Infof("consul: using target port %d from sidecar proxy configuration", w.downstream.TargetPort)
	} else {
		// Fallback: try to get it from the application service if it exists
		appSvc, _, err := w.consul.Agent().Service(w.service, &api.QueryOptions{})
		if err == nil {
			w.downstream.TargetPort = appSvc.Port
			w.log.Infof("consul: using target port %d from application service", w.downstream.TargetPort)
		} else {
			w.log.Warnf("consul: could not determine target port from proxy or application service")
		}
	}

	w.ready.Add(3) // Changed from 4 to 3 since we're not watching the app service anymore

	go w.watchCA()
	go w.watchLeaf()
	go w.watchService(proxyID, w.handleProxyChange)

	w.ready.Wait()

	for range w.update {
		w.C <- w.genCfg()
	}

	return nil
}

func (w *Watcher) handleProxyChange(first bool, srv *api.AgentService) {
	w.downstream.LocalBindAddress = DefaultDownstreamBindAddr
	w.downstream.LocalBindPort = srv.Port
	w.downstream.TargetAddress = DefaultUpstreamBindAddr
	w.downstream.ReadTimeout = DefaultReadTimeout
	w.downstream.ConnectTimeout = DefaultConnectTimeout

	if srv.Proxy != nil && srv.Proxy.Config != nil {
		if c, ok := srv.Proxy.Config["protocol"].(string); ok {
			w.downstream.Protocol = c
		}
		if b, ok := srv.Proxy.Config["bind_address"].(string); ok {
			w.downstream.LocalBindAddress = b
		}
		if a, ok := srv.Proxy.Config["local_service_address"].(string); ok {
			w.downstream.TargetAddress = a
		}
		if p, ok := srv.Proxy.Config["local_service_port"].(float64); ok {
			// Override the target port from proxy config (useful in Nomad bridge mode with port mapping)
			w.downstream.TargetPort = int(p)
			w.log.Infof("consul: overriding target port to %d from proxy config", w.downstream.TargetPort)
		}
		if f, ok := srv.Proxy.Config["enable_forwardfor"].(bool); ok {
			w.downstream.EnableForwardFor = f
		}
		if a, ok := srv.Proxy.Config["appname_header"].(string); ok {
			w.downstream.AppNameHeaderName = a
		}
		if a, ok := srv.Proxy.Config["connect_timeout"].(string); ok {
			to, err := time.ParseDuration(a)
			if err != nil {
				log.Errorf("bad connect_timeout value in config: %s. Using default: %s", err, DefaultConnectTimeout)
			} else {
				w.downstream.ConnectTimeout = to
			}
		}
		if a, ok := srv.Proxy.Config["read_timeout"].(string); ok {
			to, err := time.ParseDuration(a)
			if err != nil {
				log.Errorf("bad read_timeout value in config: %s. Using default: %s", err, DefaultReadTimeout)
			} else {
				w.downstream.ReadTimeout = to
			}
		}
	}

	keep := make(map[string]bool)

	if srv.Proxy != nil {
		for _, up := range srv.Proxy.Upstreams {
			name := fmt.Sprintf("%s_%s", up.DestinationType, up.DestinationName)
			keep[name] = true
			w.lock.Lock()
			_, ok := w.upstreams[name]
			w.lock.Unlock()
			if !ok {
				switch up.DestinationType {
				case api.UpstreamDestTypePreparedQuery:
					w.startUpstreamPreparedQuery(first, up, name)
				default:
					w.startUpstreamService(first, up, name)
				}
			} else {
				w.updateUpstream(up, w.upstreams[name])
			}
		}
	}

	for name := range w.upstreams {
		if !keep[name] {
			w.removeUpstream(name)
		}
	}

	if first {
		w.ready.Done()
	}
}

func (w *Watcher) updateUpstream(up api.Upstream, u *upstream) {
	u.LocalBindAddress = up.LocalBindAddress
	u.LocalBindPort = up.LocalBindPort
	u.Datacenter = up.Datacenter
	u.ReadTimeout = DefaultReadTimeout
	u.ConnectTimeout = DefaultConnectTimeout

	if u.LocalBindAddress == "" {
		u.LocalBindAddress = "127.0.0.1"
	}

	if p, ok := up.Config["protocol"].(string); ok {
		u.Protocol = p
	}

	if a, ok := up.Config["read_timeout"].(string); ok {
		to, err := time.ParseDuration(a)
		if err != nil {
			log.Errorf("upstream %s: bad read_timeout value in config: %s. Using default: %s", u.Name, err, DefaultReadTimeout)
		} else {
			u.ReadTimeout = to
		}
	}

	if a, ok := up.Config["connect_timeout"].(string); ok {
		to, err := time.ParseDuration(a)
		if err != nil {
			log.Errorf("upstream %s: bad connect_timeout value in config: %s. Using default: %s", u.Name, err, DefaultConnectTimeout)
		} else {
			u.ConnectTimeout = to
		}
	}
}

func (w *Watcher) startUpstreamService(startup bool, up api.Upstream, name string) {
	w.log.Infof("consul: watching upstream for service %s", up.DestinationName)

	if startup {
		w.ready.Add(1)
	}

	u := &upstream{
		Name: name,
	}

	w.updateUpstream(up, u)

	w.lock.Lock()
	w.upstreams[name] = u
	w.lock.Unlock()

	go func() {
		index := uint64(0)
		first := true
		for {
			if u.done {
				return
			}
			nodes, meta, err := w.consul.Health().Connect(up.DestinationName, "", true, &api.QueryOptions{
				Datacenter: up.Datacenter,
				WaitTime:   10 * time.Minute,
				WaitIndex:  index,
			})
			if err != nil {
				w.log.Errorf("consul: error fetching service definition for service %s: %s", up.DestinationName, err)
				time.Sleep(errorWaitTime)
				index = 0
				continue
			}
			changed := index != meta.LastIndex
			index = meta.LastIndex

			if changed {
				w.lock.Lock()
				u.Nodes = nodes
				w.lock.Unlock()
				w.notifyChanged()
			}

			if startup && first {
				w.ready.Done()
			}

			first = false
		}
	}()
}

func (w *Watcher) startUpstreamPreparedQuery(startup bool, up api.Upstream, name string) {
	w.log.Infof("consul: watching upstream for prepared_query %s", up.DestinationName)

	if startup {
		w.ready.Add(1)
	}

	u := &upstream{
		Name: name,
	}

	w.updateUpstream(up, u)

	interval := preparedQueryPollInterval
	if p, ok := up.Config["poll_interval"].(string); ok {
		dur, err := time.ParseDuration(p)
		if err != nil {
			w.log.Errorf(
				"consul: upstream %s %s: invalid poll interval %s: %s",
				up.DestinationType,
				up.DestinationName,
				p,
				err,
			)
			return
		}
		interval = dur
	}

	w.lock.Lock()
	w.upstreams[name] = u
	w.lock.Unlock()

	go func() {
		var last []*api.ServiceEntry
		first := true
		for {
			if u.done {
				return
			}
			nodes, _, err := w.consul.PreparedQuery().Execute(up.DestinationName, &api.QueryOptions{
				Connect:    true,
				Datacenter: up.Datacenter,
				WaitTime:   10 * time.Minute,
			})
			if err != nil {
				w.log.Errorf("consul: error fetching service definition for service %s: %s", up.DestinationName, err)
				time.Sleep(errorWaitTime)
				continue
			}

			nodesP := []*api.ServiceEntry{}
			for i := range nodes.Nodes {
				nodesP = append(nodesP, &nodes.Nodes[i])
			}

			if !reflect.DeepEqual(last, nodesP) {
				w.lock.Lock()
				u.Nodes = nodesP
				w.lock.Unlock()
				w.notifyChanged()
				last = nodesP
			}

			if startup && first {
				w.ready.Done()
			}

			first = false
			time.Sleep(interval)
		}
	}()
}

func (w *Watcher) removeUpstream(name string) {
	w.log.Infof("consul: removing upstream for service %s", name)

	w.lock.Lock()
	w.upstreams[name].done = true
	delete(w.upstreams, name)
	w.lock.Unlock()
}

func (w *Watcher) watchLeaf() {
	w.log.Debugf("consul: watching leaf cert for %s", w.serviceName)

	var lastIndex uint64
	first := true
	for {
		cert, meta, err := w.consul.Agent().ConnectCALeaf(w.serviceName, &api.QueryOptions{
			WaitTime:  10 * time.Minute,
			WaitIndex: lastIndex,
		})
		if err != nil {
			w.log.Errorf("consul error fetching leaf cert for service %s: %s", w.serviceName, err)
			time.Sleep(errorWaitTime)
			lastIndex = 0
			continue
		}

		changed := lastIndex != meta.LastIndex
		lastIndex = meta.LastIndex

		if changed {
			w.log.Infof("consul: leaf cert for service %s changed, serial: %s, valid before: %s, valid after: %s", w.serviceName, cert.SerialNumber, cert.ValidBefore, cert.ValidAfter)
			w.lock.Lock()
			if w.leaf == nil {
				w.leaf = &certLeaf{}
			}
			w.leaf.Cert = []byte(cert.CertPEM)
			w.leaf.Key = []byte(cert.PrivateKeyPEM)
			w.lock.Unlock()
			w.notifyChanged()
		}

		if first {
			w.log.Infof("consul: leaf cert for %s ready", w.serviceName)
			w.ready.Done()
			first = false
		}
	}
}

func (w *Watcher) watchService(service string, handler func(first bool, srv *api.AgentService)) {
	w.log.Infof("consul: watching service %s", service)

	hash := ""
	first := true
	for {
		srv, meta, err := w.consul.Agent().Service(service, &api.QueryOptions{
			WaitHash: hash,
			WaitTime: 10 * time.Minute,
		})
		if err != nil {
			w.log.Errorf("consul: error fetching service %s definition: %s", service, err)
			time.Sleep(errorWaitTime)
			hash = ""
			continue
		}

		changed := hash != meta.LastContentHash
		hash = meta.LastContentHash

		if changed {
			w.log.Debugf("consul: service %s changed", service)
			handler(first, srv)
			w.notifyChanged()
		}

		first = false
	}
}

func (w *Watcher) watchCA() {
	w.log.Debugf("consul: watching ca certs")

	first := true
	var lastIndex uint64
	for {
		caList, meta, err := w.consul.Agent().ConnectCARoots(&api.QueryOptions{
			WaitIndex: lastIndex,
			WaitTime:  10 * time.Minute,
		})
		if err != nil {
			w.log.Errorf("consul: error fetching cas: %s", err)
			time.Sleep(errorWaitTime)
			lastIndex = 0
			continue
		}

		changed := lastIndex != meta.LastIndex
		lastIndex = meta.LastIndex

		if changed {
			w.log.Infof("consul: CA certs changed, active root id: %s", caList.ActiveRootID)
			w.lock.Lock()
			w.certCAs = w.certCAs[:0]
			w.certCAPool = x509.NewCertPool()
			for _, ca := range caList.Roots {
				w.certCAs = append(w.certCAs, []byte(ca.RootCertPEM))
				ok := w.certCAPool.AppendCertsFromPEM([]byte(ca.RootCertPEM))
				if !ok {
					w.log.Warnf("consul: unable to add CA certificate to pool for root id: %s", caList.ActiveRootID)
				}
			}
			w.lock.Unlock()
			w.notifyChanged()
		}

		if first {
			w.log.Infof("consul: CA certs ready")
			w.ready.Done()
			first = false
		}
	}
}

func (w *Watcher) genCfg() Config {
	w.log.Debugf("generating configuration for service %s[%s]...", w.serviceName, w.service)
	w.lock.Lock()
	serviceInstancesAlive := 0
	serviceInstancesTotal := 0
	defer func() {
		w.lock.Unlock()
		w.log.Debugf("done generating configuration, instances: %d/%d total",
			serviceInstancesAlive, serviceInstancesTotal)
	}()

	config := Config{
		ServiceName: w.serviceName,
		ServiceID:   w.service,
		Downstream: Downstream{
			LocalBindAddress:  w.downstream.LocalBindAddress,
			LocalBindPort:     w.downstream.LocalBindPort,
			TargetAddress:     w.downstream.TargetAddress,
			TargetPort:        w.downstream.TargetPort,
			Protocol:          w.downstream.Protocol,
			ConnectTimeout:    w.downstream.ConnectTimeout,
			ReadTimeout:       w.downstream.ReadTimeout,
			EnableForwardFor:  w.downstream.EnableForwardFor,
			AppNameHeaderName: w.downstream.AppNameHeaderName,

			TLS: TLS{
				CAs:  w.certCAs,
				Cert: w.leaf.Cert,
				Key:  w.leaf.Key,
			},
		},
	}

	for _, up := range w.upstreams {
		upstream := Upstream{
			Name:             up.Name,
			LocalBindAddress: up.LocalBindAddress,
			LocalBindPort:    up.LocalBindPort,
			Protocol:         up.Protocol,
			ConnectTimeout:   up.ConnectTimeout,
			ReadTimeout:      up.ReadTimeout,
			TLS: TLS{
				CAs:  w.certCAs,
				Cert: w.leaf.Cert,
				Key:  w.leaf.Key,
			},
		}
		for _, s := range up.Nodes {
			serviceInstancesTotal++
			host := s.Service.Address
			if host == "" {
				host = s.Node.Address
			}

			weight := 1
			switch s.Checks.AggregatedStatus() {
			case api.HealthPassing:
				weight = s.Service.Weights.Passing
			case api.HealthWarning:
				weight = s.Service.Weights.Warning
			default:
				continue
			}
			if weight == 0 {
				continue
			}
			serviceInstancesAlive++

			upstream.Nodes = append(upstream.Nodes, UpstreamNode{
				Host:   host,
				Port:   s.Service.Port,
				Weight: weight,
			})
		}

		config.Upstreams = append(config.Upstreams, upstream)
	}

	sort.Slice(config.Upstreams, func(i, j int) bool {
		return config.Upstreams[i].Name < config.Upstreams[j].Name
	})

	return config
}

func (w *Watcher) notifyChanged() {
	select {
	case w.update <- struct{}{}:
	default:
	}
}
