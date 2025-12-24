package haproxy

import (
	"crypto/x509"
	"fmt"
	"sync"
	"time"

	"github.com/negasus/haproxy-spoe-go/action"
	"github.com/negasus/haproxy-spoe-go/request"
	log "github.com/sirupsen/logrus"
	"zvelo.io/ttlru"

	"github.com/haproxytech/haproxy-consul-connect/consul"
	"github.com/hashicorp/consul/agent/connect"
	"github.com/hashicorp/consul/api"
)

const (
	authzTimeout = time.Second
	cacheTTL     = time.Second
)

type cacheEntry struct {
	Value bool
	At    time.Time
	C     chan struct{}
}

type SPOEHandler struct {
	c   *api.Client
	cfg func() consul.Config

	certCache     ttlru.Cache
	authCache     map[string]*cacheEntry
	authCacheLock sync.Mutex
}

func NewSPOEHandler(c *api.Client, cfg func() consul.Config) *SPOEHandler {
	return &SPOEHandler{
		c:         c,
		cfg:       cfg,
		certCache: ttlru.New(128, ttlru.WithTTL(time.Minute)),
		authCache: map[string]*cacheEntry{},
	}
}

func (h *SPOEHandler) Handler(req *request.Request) {
	cfg := h.cfg()

	// Get the check-intentions message
	msg, err := req.Messages.GetByName("check-intentions")
	if err != nil {
		log.Errorf("spoe handler: message 'check-intentions' not found: %s", err)
		return
	}

	// Get the cert argument
	certValue, ok := msg.KV.Get("cert")
	if !ok {
		log.Error("spoe handler: cert argument is required")
		return
	}

	certBytes, ok := certValue.([]byte)
	if !ok {
		log.Errorf("spoe handler: expected cert bytes, got: %T", certValue)
		return
	}

	cert, err := h.decodeCertificate(certBytes)
	if err != nil {
		log.Errorf("spoe handler: %s", err)
		return
	}

	if len(cert.URIs) == 0 {
		log.Error("spoe handler: certificate has no URIs")
		return
	}

	certURI, err := connect.ParseCertURI(cert.URIs[0])
	if err != nil {
		log.Error("connect: invalid leaf certificate URI")
		return
	}

	sourceApp := ""
	authorized, err := h.isAuthorized(cfg.ServiceName, certURI.URI().String(), cert.SerialNumber.Bytes())
	if err != nil {
		log.Errorf("spoe handler: %s", err)
		return
	}

	if sis, ok := certURI.(*connect.SpiffeIDService); ok {
		sourceApp = sis.Service
	}

	res := 1
	if !authorized {
		res = 0
	}

	// Set variables using the new API
	req.Actions.SetVar(action.ScopeSession, "auth", res)
	req.Actions.SetVar(action.ScopeSession, "source_app", sourceApp)
}

func (h *SPOEHandler) isAuthorized(target, uri string, serial []byte) (bool, error) {
	h.authCacheLock.Lock()
	entry, ok := h.authCache[uri]
	now := time.Now()
	if !ok || now.Sub(entry.At) > cacheTTL {
		entry = &cacheEntry{
			At: now,
			C:  make(chan struct{}),
		}
		h.authCache[uri] = entry
		h.authCacheLock.Unlock()

		go func() {
			auth, err := h.fetchAutz(target, uri, serial)

			h.authCacheLock.Lock()
			defer h.authCacheLock.Unlock()

			if err != nil {
				log.Error(err)
				entry.Value = false
				// force refech on next request
				entry.At = time.Time{}
			} else {
				entry.Value = auth
			}

			// notify waiting requets
			close(entry.C)
		}()
	} else {
		h.authCacheLock.Unlock()
	}

	select {
	case <-time.After(authzTimeout):
		return false, fmt.Errorf("authz call failed: timeout after %s", authzTimeout)
	case <-entry.C:
		return entry.Value, nil
	}
}

func (h *SPOEHandler) fetchAutz(target, uri string, serial []byte) (bool, error) {
	resp, err := h.c.Agent().ConnectAuthorize(&api.AgentAuthorizeParams{
		Target:           target,
		ClientCertURI:    uri,
		ClientCertSerial: connect.HexString(serial),
	})
	if err != nil {
		return false, fmt.Errorf("authz call failed: %w", err)
	}

	return resp.Authorized, nil
}

func (h *SPOEHandler) decodeCertificate(b []byte) (*x509.Certificate, error) {
	certCacheKey := string(b)
	if v, ok := h.certCache.Get(certCacheKey); ok {
		return v.(*x509.Certificate), nil
	}

	cert, err := x509.ParseCertificate(b)
	if err != nil {
		return nil, err
	}
	h.certCache.Set(certCacheKey, cert)

	return cert, nil
}
