package state

import (
	"fmt"
	"sort"

	"github.com/haproxytech/models/v2"
)

type HAProxyRead interface {
	Frontends() ([]models.Frontend, error)
	Binds(feName string) ([]models.Bind, error)
	LogTargets(parentType, parentName string) ([]models.LogTarget, error)
	Filters(parentType, parentName string) ([]models.Filter, error)
	TCPRequestRules(parentType, parentName string) ([]models.TCPRequestRule, error)
	HTTPRequestRules(parentType, parentName string) ([]models.HTTPRequestRule, error)
	Backends() ([]models.Backend, error)
	Servers(beName string) ([]models.Server, error)
}

func FromHAProxy(ha HAProxyRead) (State, error) {
	state := State{}

	haFrontends, err := ha.Frontends()
	if err != nil {
		return state, err
	}

	for _, f := range haFrontends {
		binds, err := ha.Binds(f.Name)
		if err != nil {
			return state, err
		}
		if len(binds) != 1 {
			return state, fmt.Errorf("expected 1 bind for frontend %s, got %d", f.Name, len(binds))
		}
		logTargets, err := ha.LogTargets("frontend", f.Name)
		if err != nil {
			return state, err
		}
		if len(logTargets) > 1 {
			return state, fmt.Errorf("expected at most 1 log target for frontend %s, got %d", f.Name, len(logTargets))
		}

		var lt *models.LogTarget
		if len(logTargets) == 1 {
			lt = &logTargets[0]
		}

		filters, err := ha.Filters("frontend", f.Name)
		if err != nil {
			return state, err
		}
		var filterSpoe *FrontendFilter
		var filterCompression *FrontendFilter
		for _, filter := range filters {
			switch filter.Type {
			case models.FilterTypeSpoe:
				{
					if filterSpoe != nil {
						return state, fmt.Errorf("spoe filter already initialized for frontend %s", f.Name)
					}
					filterSpoe = &FrontendFilter{
						Filter: filter,
					}
					rules, err := ha.TCPRequestRules("frontend", f.Name)
					if err != nil {
						return state, err
					}
					if len(binds) != 1 {
						return state, fmt.Errorf("expected 1 tcp request rule for frontend %s, got %d", f.Name, len(rules))
					}
					filterSpoe.Rule = rules[0]
				}
			case models.FilterTypeCompression:
				{
					if filterCompression != nil {
						return state, fmt.Errorf("compression filter already initialized for frontend %s", f.Name)
					}
					filterCompression = &FrontendFilter{
						Filter: filter,
					}
				}
			default:
				fmt.Errorf("unknown filter type for frontend %s, got %s", f.Name, filter.Type)
			}
		}

		state.Frontends = append(state.Frontends, Frontend{
			Frontend:          f,
			Bind:              binds[0],
			LogTarget:         lt,
			FilterSpoe:        filterSpoe,
			FilterCompression: filterCompression,
		})
	}

	sort.Sort(Frontends(state.Frontends))

	haBackends, err := ha.Backends()
	if err != nil {
		return state, err
	}

	for _, b := range haBackends {
		servers, err := ha.Servers(b.Name)
		if err != nil {
			return state, err
		}

		logTargets, err := ha.LogTargets("backend", b.Name)
		if err != nil {
			return state, err
		}
		if len(logTargets) > 1 {
			return state, fmt.Errorf("expected at most 1 log target for backend %s, got %d", b.Name, len(logTargets))
		}

		var lt *models.LogTarget
		if len(logTargets) == 1 {
			lt = &logTargets[0]
		}

		reqRules, err := ha.HTTPRequestRules("backend", b.Name)
		if err != nil {
			return state, err
		}
		if len(reqRules) == 0 {
			reqRules = nil
		}

		state.Backends = append(state.Backends, Backend{
			Backend:          b,
			Servers:          servers,
			LogTarget:        lt,
			HTTPRequestRules: reqRules,
		})
	}

	sort.Sort(Backends(state.Backends))

	return state, nil
}
