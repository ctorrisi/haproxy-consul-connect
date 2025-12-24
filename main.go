package main

import (
	"flag"
	"fmt"
	"github.com/haproxytech/haproxy-consul-connect/haproxy"
	"os"
	"path/filepath"
	"strings"

	log "github.com/sirupsen/logrus"

	"github.com/haproxytech/haproxy-consul-connect/haproxy/haproxy_cmd"
	"github.com/haproxytech/haproxy-consul-connect/lib"
	"github.com/haproxytech/haproxy-consul-connect/utils"

	"github.com/hashicorp/consul/api"

	"github.com/haproxytech/haproxy-consul-connect/consul"
)

// Version is set by Travis build
var Version string = "v0.1.9-Dev"

// BuildTime is set by Travis
var BuildTime string = "2020-01-01T00:00:00Z"

// GitHash The last reference Hash from Git
var GitHash string = "unknown"

type consulLogger struct{}

// Debugf Display debug message
func (consulLogger) Debugf(format string, args ...interface{}) {
	log.Debugf(format, args...)
}

// Infof Display info message
func (consulLogger) Infof(format string, args ...interface{}) {
	log.Infof(format, args...)
}

// Warnf Display warning message
func (consulLogger) Warnf(format string, args ...interface{}) {
	log.Infof(format, args...)
}

// Errorf Display error message
func (consulLogger) Errorf(format string, args ...interface{}) {
	log.Errorf(format, args...)
}

// validateRequirements Checks that dependencies are present
func validateRequirements(haproxyBin string) error {
	err := haproxy_cmd.CheckEnvironment(haproxyBin)
	if err != nil {
		msg := fmt.Sprintf("Some external dependencies are missing: %s", err.Error())
		os.Stderr.WriteString(fmt.Sprintf("%s\n", msg))
		return err
	}
	return nil
}

func main() {
	haproxyParamsFlag := utils.StringSliceFlag{}

	flag.Var(&haproxyParamsFlag, "haproxy-param", "Global or defaults Haproxy config parameter to set in config. Can be specified multiple times. Must be of the form `defaults.name=value` or `global.name=value`")
	versionFlag := flag.Bool("version", false, "Show version and exit")
	logLevel := flag.String("log-level", "INFO", "Log level")
	consulAddr := flag.String("http-addr", "127.0.0.1:8500", "Consul agent address")
	service := flag.String("sidecar-for", "", "The consul service id to proxy")
	serviceTag := flag.String("sidecar-for-tag", "", "The consul service id to proxy")
	haproxyBin := flag.String("haproxy", haproxy_cmd.DefaultHAProxyBin, "Haproxy binary path")
	haproxyCfgBasePath := flag.String("haproxy-cfg-base-path", "/tmp", "Haproxy binary path")
	statsListenAddr := flag.String("stats-addr", "", "Listen addr for stats server")
	statsServiceRegister := flag.Bool("stats-service-register", false, "Register a consul service for connect stats")
	enableIntentions := flag.Bool("enable-intentions", false, "Enable Connect intentions")
	token := flag.String("token", "", "Consul ACL token")
	envoyBootstrapPath := flag.String("envoy-bootstrap", "", "Path to Envoy bootstrap file (optional, for extracting Consul token)")
	flag.Parse()
	if versionFlag != nil && *versionFlag {
		fmt.Printf("Version: %s ; BuildTime: %s ; GitHash: %s\n", Version, BuildTime, GitHash)
		os.Exit(0)
	}
	if err := validateRequirements(*haproxyBin); err != nil {
		fmt.Printf("ERROR: HAProxy dependencies are not satisfied: %s\n", err)
		os.Exit(4)
	}

	ll, err := log.ParseLevel(*logLevel)
	if err != nil {
		log.Fatal(err)
	}
	log.SetLevel(ll)

	sd := lib.NewShutdown()

	// Auto-detect Nomad secrets directory if envoy-bootstrap not explicitly set
	if *envoyBootstrapPath == "" {
		if nomadSecretsDir := os.Getenv("NOMAD_SECRETS_DIR"); nomadSecretsDir != "" {
			nomadBootstrapPath := filepath.Join(nomadSecretsDir, "envoy_bootstrap.json")
			if _, err := os.Stat(nomadBootstrapPath); err == nil {
				*envoyBootstrapPath = nomadBootstrapPath
				log.Infof("Auto-detected Envoy bootstrap file from NOMAD_SECRETS_DIR: %s", nomadBootstrapPath)
			}
		}
	}

	// Try to parse Envoy bootstrap file if provided
	var bootstrapConfig *utils.EnvoyBootstrapConfig
	if *envoyBootstrapPath != "" {
		bootstrapConfig, err = utils.ParseEnvoyBootstrap(*envoyBootstrapPath)
		if err != nil {
			log.Warnf("Failed to parse envoy bootstrap file: %s", err)
		} else if bootstrapConfig != nil {
			log.Info("Successfully parsed Envoy bootstrap configuration")
		}
	}

	consulConfig := &api.Config{
		Address: *consulAddr,
	}

	// Token priority (lowest to highest):
	// 1. Envoy bootstrap file
	// 2. Environment variable
	// 3. Command line flag
	if bootstrapConfig != nil {
		if bootstrapToken := bootstrapConfig.ExtractConsulToken(); bootstrapToken != "" {
			consulConfig.Token = bootstrapToken
			log.Info("Setting token from Envoy bootstrap file")
		}
	}
	env_token, env_token_exists := os.LookupEnv("CONNECT_CONSUL_TOKEN")
	if env_token_exists {
		consulConfig.Token = env_token
		log.Info("Setting token from env variable CONNECT_CONSUL_TOKEN")
	}
	if *token != "" {
		consulConfig.Token = *token
		log.Info("Setting token from command line")
	}
	consulClient, err := api.NewClient(consulConfig)
	if err != nil {
	}

	var serviceID string
	if *serviceTag != "" {
		svcs, err := consulClient.Agent().Services()
		if err != nil {
			log.Fatal(err)
		}
	OUTER:
		for _, s := range svcs {
			if strings.HasSuffix(s.Service, "sidecar-proxy") {
				continue
			}
			for _, t := range s.Tags {
				if t == *serviceTag {
					serviceID = s.ID
					break OUTER
				}
			}
		}
		if serviceID == "" {
			log.Fatalf("No sidecar proxy found for service with tag %s", *serviceTag)
		}
	} else if *service != "" {
		serviceID = *service
	} else if bootstrapConfig != nil {
		// Try to extract service name from Envoy bootstrap
		if extractedService := bootstrapConfig.ExtractServiceName(); extractedService != "" {
			serviceID = extractedService
			log.Infof("Using service name from Envoy bootstrap: %s", serviceID)
		} else {
			log.Fatalf("Please specify -sidecar-for, -sidecar-for-tag, or provide -envoy-bootstrap with valid service information")
		}
	} else {
		log.Fatalf("Please specify -sidecar-for, -sidecar-for-tag, or provide -envoy-bootstrap with valid service information")
	}

	haproxyParams, err := utils.MakeHAProxyParams(haproxyParamsFlag)
	if err != nil {
		log.Fatal(err)
	}

	consulLogger := &consulLogger{}
	watcher := consul.New(serviceID, consulClient, consulLogger)
	go func() {
		if err := watcher.Run(); err != nil {
			log.Error(err)
			sd.Shutdown(err.Error())
		}
	}()

	hap := haproxy.New(consulClient, watcher.C, utils.Options{
		HAProxyBin:           *haproxyBin,
		ConfigBaseDir:        *haproxyCfgBasePath,
		EnableIntentions:     *enableIntentions,
		StatsListenAddr:      *statsListenAddr,
		StatsRegisterService: *statsServiceRegister,
		LogRequests:          ll == log.TraceLevel,
		HAProxyParams:        haproxyParams,
	})
	sd.Add(1)
	go func() {
		defer sd.Done()
		if err := hap.Run(sd); err != nil {
			log.Error(err)
			sd.Shutdown(err.Error())
		}
	}()

	sd.Wait()
}
