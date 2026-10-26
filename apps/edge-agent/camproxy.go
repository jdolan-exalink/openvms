package main

import (
	"net"
	"net/http"
	"strconv"

	"github.com/jdolan-exalink/openvms/internal/agent/camproxy"
	"github.com/jdolan-exalink/openvms/internal/agent/tokens"
)

// ownListenPorts lists the ports the camera web proxy must never reach: the defaults of the control
// and mTLS listeners plus whatever the configured listen addresses say.
func ownListenPorts(listenAddrs ...string) []int {
	ports := []int{7419, 7443}
	for _, addr := range listenAddrs {
		if _, p, err := net.SplitHostPort(addr); err == nil {
			if n, err := strconv.Atoi(p); err == nil && n > 0 {
				ports = append(ports, n)
			}
		}
	}
	return ports
}

// mountCameraWeb serves the camera web view proxy behind the control scope. The control plane sends
// the camera in X-OpenVMS-Target headers; see internal/agent/camproxy for the contract and the limits.
func mountCameraWeb(mux *http.ServeMux, auth *authenticator, proxy *camproxy.Handler) {
	mux.Handle(camproxy.Path, auth.scoped(tokens.ScopeControl, proxy))
}
