package inventory

import (
	"context"
	"errors"
	"net"
	"strconv"
	"strings"

	"github.com/google/uuid"

	"github.com/jdolan-exalink/openvms/internal/access"
	"github.com/jdolan-exalink/openvms/internal/authz"
	"github.com/jdolan-exalink/openvms/internal/provisionnvr"
	"github.com/jdolan-exalink/openvms/internal/store/db"
)

// NVRError is a failed call to a Provision-ISR recorder. Class says what the caller should tell the
// user; the underlying error stays out of the message because it may describe the recorder's answer.
type NVRError struct {
	Class provisionnvr.Class
	Err   error
}

func (e *NVRError) Error() string { return "provision NVR: " + string(e.Class) }
func (e *NVRError) Unwrap() error { return e.Err }

// NVRConnInput is how a recorder is reached. Certificates are always verified when HTTPS is used.
type NVRConnInput struct {
	Host     string
	Port     int
	HTTPS    bool
	Username string
	Password string
}

func (in NVRConnInput) validate() error {
	host := strings.TrimSpace(in.Host)
	if host == "" || strings.ContainsAny(host, "/\\?#@ \t\r\n") || strings.Contains(host, "://") {
		return invalid("host must be a host name or IP address, without scheme, port or path")
	}
	if strings.Count(host, ":") == 1 {
		return invalid("host must not include a port; use the port field")
	}
	if in.Port < 0 || in.Port > 65535 {
		return invalid("port must be between 1 and 65535")
	}
	if in.Username == "" || in.Password == "" {
		return invalid("username and password are required")
	}
	return nil
}

func (in NVRConnInput) baseURL() string {
	port := in.Port
	scheme := "http"
	if in.HTTPS {
		scheme = "https"
	}
	if port == 0 {
		port = 80
		if in.HTTPS {
			port = 443
		}
	}
	host := strings.Trim(strings.TrimSpace(in.Host), "[]")
	return scheme + "://" + net.JoinHostPort(host, strconv.Itoa(port))
}

// DetectProvisionNvr reads a recorder's identity, channels, storage and capabilities without saving
// anything. Needs servers.manage on the site. The password is used for this call and not kept.
func (s *Service) DetectProvisionNvr(ctx context.Context, actor authz.Actor, siteID uuid.UUID, conn NVRConnInput) (*provisionnvr.Detection, error) {
	if err := conn.validate(); err != nil {
		return nil, err
	}
	err := s.tx(ctx, actor, func(q *db.Queries, c *access.Checker) error {
		_, err := requireSite(ctx, q, c, authz.ServersManage, siteID)
		return err
	})
	if err != nil {
		return nil, err
	}
	det, err := provisionnvr.Detect(ctx, provisionnvr.DetectInput{BaseURL: conn.baseURL(), Username: conn.Username, Password: conn.Password})
	if err != nil {
		var ne *NVRError
		if errors.As(err, &ne) {
			return nil, err
		}
		if class, ok := provisionnvr.ClassOf(err); ok {
			return nil, &NVRError{Class: class, Err: err}
		}
		if errors.Is(err, provisionnvr.ErrInvalidEndpoint) {
			return nil, invalid("host or port is not valid")
		}
		return nil, &NVRError{Class: provisionnvr.ClassOffline, Err: err}
	}
	return det, nil
}
