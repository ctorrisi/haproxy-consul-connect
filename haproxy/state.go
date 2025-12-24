package haproxy

import (
	"time"

	"github.com/haproxytech/haproxy-consul-connect/consul"
	"github.com/haproxytech/haproxy-consul-connect/haproxy/renderer"
	"github.com/haproxytech/haproxy-consul-connect/haproxy/state"
	"github.com/haproxytech/haproxy-consul-connect/lib"
	log "github.com/sirupsen/logrus"
)

const (
	stateApplyThrottle = 500 * time.Millisecond
	retryBackoff       = 3 * time.Second
)

func (h *HAProxy) watch(sd *lib.Shutdown) error {
	throttle := time.Tick(stateApplyThrottle)
	retry := make(chan struct{})

	var currentState state.State
	var currentConfig consul.Config
	started := false
	ready := false

	waitAndRetry := func() {
		time.Sleep(retryBackoff)
		select {
		case retry <- struct{}{}:
		default:
		}
	}

	for {
		inputReceived := false
	Throttle:
		for {
			select {
			case <-sd.Stop:
				return nil

			case <-throttle:
				if inputReceived {
					break Throttle
				}

			case c := <-h.cfgC:
				log.Info("handling new configuration")
				h.currentConsulConfig = &c
				currentConfig = c
				inputReceived = true
			case <-retry:
				log.Warn("retrying to apply config")
				inputReceived = true
			}
		}

		if !started {
			err := h.start(sd)
			if err != nil {
				return err
			}
			started = true
		}

		newState, err := state.Generate(state.Options{
			EnableIntentions: h.opts.EnableIntentions,
			LogRequests:      h.opts.LogRequests,
			LogSocket:        h.haConfig.LogsSock,
			SPOEConfigPath:   h.haConfig.SPOE,
			SPOESocket:       h.haConfig.SPOESock,
		}, h.haConfig, currentState, currentConfig)
		if err != nil {
			log.Error(err)
			continue
		}

		if currentState.Equal(newState) {
			log.Info("no change to apply to haproxy")
			continue
		}

		log.Debugf("applying new state: %+v", newState)

		// Render config
		config, err := h.renderer.Render(newState, h.haConfig.StatsSock, renderer.HAProxyParams{
			Globals:  h.opts.HAProxyParams.Globals,
			Defaults: h.opts.HAProxyParams.Defaults,
		})
		if err != nil {
			log.Errorf("failed to render config: %s", err)
			waitAndRetry()
			continue
		}

		// Apply config
		err = h.configWriter.ApplyConfig(config)
		if err != nil {
			log.Errorf("failed to apply config: %s", err)
			waitAndRetry()
			continue
		}

		if !ready {
			close(h.Ready)
			ready = true
		}

		currentState = newState
		log.Info("state applied")
	}
}
