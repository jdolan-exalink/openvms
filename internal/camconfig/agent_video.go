package camconfig

import (
	"context"
	"errors"
	"fmt"

	"github.com/jdolan-exalink/openvms/internal/agent/onvifdiscover"
	"github.com/jdolan-exalink/openvms/internal/onvif"
)

// VideoAgentOutdated is the reason when the server's agent does not know the video operations yet.
const VideoAgentOutdated = "agent_outdated"

// ErrAgentOutdated means the agent behind the camera is older than the video operations: it has to
// be updated before this camera's video can be read or changed.
var ErrAgentOutdated = errors.New("the server's agent is too old for the video operations")

// AgentVideoCall sends one video-device operation to the agent of the camera's server and returns
// what it answered with the HTTP status. The API supplies it (it owns the permission checks and the
// agent connection); tests supply fakes. It returns ErrAgentOutdated when the agent has no such
// operation and any other error when the agent cannot be reached or trusted.
type AgentVideoCall func(ctx context.Context, req onvifdiscover.VideoDeviceRequest) (onvifdiscover.VideoDeviceResult, int, error)

// AgentVideoDevice is a VideoDevice that reaches the camera through its server's agent, so a camera
// on a LAN that only the agent can see can still be read and changed. The credentials are held for
// the requests and only travel to the agent; no error carries them.
type AgentVideoDevice struct {
	Endpoint string
	Username string
	Password string
	Call     AgentVideoCall
}

func (d *AgentVideoDevice) request(op, token string) onvifdiscover.VideoDeviceRequest {
	return onvifdiscover.VideoDeviceRequest{Endpoint: d.Endpoint, Username: d.Username, Password: d.Password, Op: op, ProfileToken: token}
}

// Streams returns the current video settings of every media profile.
func (d *AgentVideoDevice) Streams(ctx context.Context) ([]onvif.ProfileStream, error) {
	res, err := d.run(ctx, d.request(onvifdiscover.VideoDeviceOpStreams, ""))
	if err != nil {
		return nil, err
	}
	return res.Streams, nil
}

// Options asks which video values the profile supports.
func (d *AgentVideoDevice) Options(ctx context.Context, profileToken string) (onvif.VideoOptions, error) {
	res, err := d.run(ctx, d.request(onvifdiscover.VideoDeviceOpOptions, profileToken))
	if err != nil {
		return onvif.VideoOptions{}, err
	}
	if res.Options == nil {
		return onvif.VideoOptions{}, errors.New("agent: the video options answer is empty")
	}
	return *res.Options, nil
}

// Set changes the requested fields of a profile; the agent reads, writes and verifies.
func (d *AgentVideoDevice) Set(ctx context.Context, profileToken string, desired onvif.VideoDesired) (onvif.ProfileStream, error) {
	req := d.request(onvifdiscover.VideoDeviceOpSet, profileToken)
	req.Desired = &desired
	res, err := d.run(ctx, req)
	var stream onvif.ProfileStream
	if res.Stream != nil {
		stream = *res.Stream
	}
	if err != nil {
		return stream, err
	}
	if res.Stream == nil {
		return stream, errors.New("agent: the set answer is empty")
	}
	return stream, nil
}

func (d *AgentVideoDevice) run(ctx context.Context, req onvifdiscover.VideoDeviceRequest) (onvifdiscover.VideoDeviceResult, error) {
	if d.Call == nil {
		return onvifdiscover.VideoDeviceResult{}, errors.New("agent: no connection to the agent")
	}
	res, _, err := d.Call(ctx, req)
	if err != nil {
		return onvifdiscover.VideoDeviceResult{}, err
	}
	if res.Success {
		return res, nil
	}
	return res, agentVideoError(res)
}

// agentVideoError turns an agent answer back into the typed error the building blocks return when
// the camera is reached directly, so the review and apply code treats both paths alike. Nothing of
// the camera's own text is kept.
func agentVideoError(res onvifdiscover.VideoDeviceResult) error {
	switch res.Code {
	case onvifdiscover.VideoDeviceNotSupported:
		return &onvif.ValidationError{Violations: res.Violations}
	case onvifdiscover.VideoDeviceNotApplied:
		return &onvif.VerifyError{Fields: res.Fields}
	case onvifdiscover.VideoDeviceProfileNotFound:
		return onvif.ErrProfileNotFound
	case onvifdiscover.VideoDeviceNoEncoder:
		return onvif.ErrNoVideoEncoder
	case onvifdiscover.VideoDeviceGOPNotWritable:
		return onvif.ErrGOPNotWritable
	case onvifdiscover.VideoDeviceUnknownOp:
		return ErrAgentOutdated
	}
	return fmt.Errorf("agent: the camera could not be reached (%s)", res.Code)
}
