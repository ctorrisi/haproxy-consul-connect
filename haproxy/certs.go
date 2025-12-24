package haproxy

import (
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"os"
	"path"
	"strings"

	"github.com/haproxytech/haproxy-consul-connect/consul"
	log "github.com/sirupsen/logrus"
)

func (h *haConfig) FilePath(content []byte) (string, error) {
	sum := sha256.Sum256(content)

	path := path.Join(h.Base, hex.EncodeToString(sum[:]))

	_, err := os.Stat(path)
	if err != nil && !os.IsNotExist(err) {
		return "", err
	}

	if err == nil {
		return path, nil
	}

	f, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
	if err != nil {
		return "", err
	}
	defer func() {
		err := f.Close()
		if err != nil {
			log.Errorf("error closing file %s: %s", path, err)
		}
	}()

	_, err = f.Write(content)
	if err != nil {
		return "", err
	}

	log.Debugf("wrote new config file %s", path)

	return path, nil
}

func inspectCertificate(certPEM []byte, label string) {
	block, _ := pem.Decode(certPEM)
	if block == nil {
		log.Warnf("%s: failed to decode PEM certificate", label)
		return
	}

	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		log.Warnf("%s: failed to parse certificate: %s", label, err)
		return
	}

	log.Infof("%s certificate details:", label)
	log.Infof("  Subject: %s", cert.Subject.String())
	log.Infof("  Issuer: %s", cert.Issuer.String())

	if len(cert.DNSNames) > 0 {
		log.Infof("  DNS SANs: %s", strings.Join(cert.DNSNames, ", "))
	}

	if len(cert.URIs) > 0 {
		uris := make([]string, len(cert.URIs))
		for i, uri := range cert.URIs {
			uris[i] = uri.String()
		}
		log.Infof("  URI SANs: %s", strings.Join(uris, ", "))
	}

	if len(cert.IPAddresses) > 0 {
		ips := make([]string, len(cert.IPAddresses))
		for i, ip := range cert.IPAddresses {
			ips[i] = ip.String()
		}
		log.Infof("  IP SANs: %s", strings.Join(ips, ", "))
	}
}

func (h *haConfig) CertsPath(t consul.TLS) (string, string, error) {
	// Inspect the leaf certificate
	if len(t.Cert) > 0 {
		inspectCertificate(t.Cert, "Leaf")
	}

	// Inspect CA certificates
	for i, ca := range t.CAs {
		inspectCertificate(ca, fmt.Sprintf("CA[%d]", i))
	}

	crt := []byte{}
	crt = append(crt, t.Cert...)
	crt = append(crt, t.Key...)

	crtPath, err := h.FilePath(crt)
	if err != nil {
		return "", "", err
	}

	ca := []byte{}
	for _, c := range t.CAs {
		ca = append(ca, c...)
	}

	caPath, err := h.FilePath(ca)
	if err != nil {
		return "", "", err
	}

	return caPath, crtPath, nil
}
