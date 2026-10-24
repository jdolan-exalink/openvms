package main

import (
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jdolan-exalink/openvms/internal/agent/tokens"
)

type agentTLSConfig struct {
	Addr         string
	Certificates []tls.Certificate
}

func loadAgentTLSConfig(listen, certFile, keyFile string) (*agentTLSConfig, bool, error) {
	listen, certFile, keyFile = strings.TrimSpace(listen), strings.TrimSpace(certFile), strings.TrimSpace(keyFile)
	if listen == "" && certFile == "" && keyFile == "" {
		return nil, false, nil
	}
	if listen == "" || certFile == "" || keyFile == "" {
		return nil, false, errors.New("ONVIF TLS requires OPENVMS_AGENT_ONVIF_TLS_LISTEN, TLS_CERT_FILE, and TLS_KEY_FILE")
	}
	host, port, err := net.SplitHostPort(listen)
	if err != nil || host == "" || port == "" {
		return nil, false, errors.New("invalid OPENVMS_AGENT_ONVIF_TLS_LISTEN address")
	}
	n, err := strconv.Atoi(port)
	if err != nil || n < 1 || n > 65535 {
		return nil, false, errors.New("invalid OPENVMS_AGENT_ONVIF_TLS_LISTEN port")
	}
	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return nil, false, fmt.Errorf("load ONVIF TLS certificate pair: %w", err)
	}
	return &agentTLSConfig{Addr: listen, Certificates: []tls.Certificate{cert}}, true, nil
}

func buildTLSMux(auth *authenticator, healthHandler, discoveryHandler, probeHandler, lprHandler http.Handler) *http.ServeMux {
	mux := http.NewServeMux()
	if healthHandler != nil {
		mux.Handle("GET /v1/health", auth.scoped(tokens.ScopeRead, healthHandler))
	}
	if discoveryHandler != nil {
		mux.Handle("POST /v1/onvif/discover", auth.scoped(tokens.ScopeControl, discoveryHandler))
	}
	if probeHandler != nil {
		mux.Handle("POST /v1/onvif/probe", auth.scoped(tokens.ScopeControl, probeHandler))
	}
	if lprHandler != nil {
		mountProvisionLPR(mux, auth, lprHandler)
	}
	return mux
}

func newAgentTLSServer(cfg *agentTLSConfig, auth *authenticator, healthHandler, discoveryHandler, probeHandler, lprHandler http.Handler) *http.Server {
	return &http.Server{
		Addr:              cfg.Addr,
		Handler:           buildTLSMux(auth, healthHandler, discoveryHandler, probeHandler, lprHandler),
		ReadHeaderTimeout: 5 * time.Second,
		TLSConfig:         &tls.Config{MinVersion: tls.VersionTLS12, Certificates: cfg.Certificates},
	}
}
