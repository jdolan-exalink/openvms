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

func buildTLSMux(healthHandler, discoveryHandler, probeHandler http.Handler) *http.ServeMux {
	mux := http.NewServeMux()
	if healthHandler != nil {
		mux.Handle("GET /v1/health", healthHandler)
	}
	if discoveryHandler != nil {
		mux.Handle("POST /v1/onvif/discover", discoveryHandler)
	}
	if probeHandler != nil {
		mux.Handle("POST /v1/onvif/probe", probeHandler)
	}
	return mux
}

func newAgentTLSServer(cfg *agentTLSConfig, healthHandler, discoveryHandler, probeHandler http.Handler) *http.Server {
	return &http.Server{
		Addr:              cfg.Addr,
		Handler:           buildTLSMux(healthHandler, discoveryHandler, probeHandler),
		ReadHeaderTimeout: 5 * time.Second,
		TLSConfig:         &tls.Config{MinVersion: tls.VersionTLS12, Certificates: cfg.Certificates},
	}
}
