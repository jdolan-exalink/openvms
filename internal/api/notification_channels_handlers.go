package api

import (
	"context"

	"github.com/jdolan-exalink/openvms/internal/api/gen"
	"github.com/jdolan-exalink/openvms/internal/notify"
)

func toChannel(c notify.Channel) gen.NotificationChannel {
	cfg := gen.NotificationChannelConfig{}
	if c.Config.URL != "" {
		cfg.Url = &c.Config.URL
	}
	if c.Config.Host != "" {
		cfg.Host = &c.Config.Host
	}
	if c.Config.Port != 0 {
		cfg.Port = &c.Config.Port
	}
	if c.Config.TLS != "" {
		tls := gen.NotificationChannelConfigTls(c.Config.TLS)
		cfg.Tls = &tls
	}
	if c.Config.Username != "" {
		cfg.Username = &c.Config.Username
	}
	if c.Config.From != "" {
		cfg.From = &c.Config.From
	}
	if len(c.Config.Recipients) > 0 {
		cfg.Recipients = &c.Config.Recipients
	}
	if c.Config.Session != "" {
		cfg.Session = &c.Config.Session
	}
	if len(c.Config.ChatIDs) > 0 {
		cfg.ChatIds = &c.Config.ChatIDs
	}
	set := c.SecretsSet
	if set == nil {
		set = []string{}
	}
	return gen.NotificationChannel{
		Id: c.ID, TenantId: c.TenantID, Name: c.Name, Type: gen.NotificationChannelType(c.Type),
		Enabled: c.Enabled, Config: cfg, SecretsSet: set, CreatedAt: c.CreatedAt, UpdatedAt: c.UpdatedAt,
	}
}

func fromChannelConfig(c *gen.NotificationChannelConfig) notify.Config {
	var out notify.Config
	if c == nil {
		return out
	}
	if c.Url != nil {
		out.URL = *c.Url
	}
	if c.Host != nil {
		out.Host = *c.Host
	}
	if c.Port != nil {
		out.Port = *c.Port
	}
	if c.Tls != nil {
		out.TLS = string(*c.Tls)
	}
	if c.Username != nil {
		out.Username = *c.Username
	}
	if c.From != nil {
		out.From = *c.From
	}
	if c.Recipients != nil {
		out.Recipients = *c.Recipients
	}
	if c.Session != nil {
		out.Session = *c.Session
	}
	if c.ChatIds != nil {
		out.ChatIDs = *c.ChatIds
	}
	return out
}

func fromChannelSecrets(s *gen.NotificationChannelSecrets) notify.Secrets {
	var out notify.Secrets
	if s == nil {
		return out
	}
	if s.SigningSecret != nil {
		out.SigningSecret = *s.SigningSecret
	}
	if s.Headers != nil {
		out.Headers = *s.Headers
	}
	if s.SmtpPassword != nil {
		out.SMTPPassword = *s.SmtpPassword
	}
	if s.BotToken != nil {
		out.BotToken = *s.BotToken
	}
	return out
}

func (h *Handlers) ListNotificationChannels(ctx context.Context, _ gen.ListNotificationChannelsRequestObject) (gen.ListNotificationChannelsResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	items, err := h.Notify.List(ctx, a)
	if err != nil {
		return nil, err
	}
	out := make([]gen.NotificationChannel, len(items))
	for i, c := range items {
		out[i] = toChannel(c)
	}
	return gen.ListNotificationChannels200JSONResponse{Items: out}, nil
}

func (h *Handlers) CreateNotificationChannel(ctx context.Context, r gen.CreateNotificationChannelRequestObject) (gen.CreateNotificationChannelResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	if r.Body == nil {
		return nil, &notify.ValidationError{Msg: "missing request body"}
	}
	enabled := true
	if r.Body.Enabled != nil {
		enabled = *r.Body.Enabled
	}
	item, err := h.Notify.Create(ctx, a, notify.CreateRequest{
		Name: r.Body.Name, Type: notify.Type(r.Body.Type), Enabled: enabled,
		Config: fromChannelConfig(r.Body.Config), Secrets: fromChannelSecrets(r.Body.Secrets),
	})
	if err != nil {
		return nil, err
	}
	return gen.CreateNotificationChannel201JSONResponse(toChannel(item)), nil
}

func (h *Handlers) GetNotificationChannel(ctx context.Context, r gen.GetNotificationChannelRequestObject) (gen.GetNotificationChannelResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	item, err := h.Notify.Get(ctx, a, r.ChannelId)
	if err != nil {
		return nil, err
	}
	return gen.GetNotificationChannel200JSONResponse(toChannel(item)), nil
}

func (h *Handlers) UpdateNotificationChannel(ctx context.Context, r gen.UpdateNotificationChannelRequestObject) (gen.UpdateNotificationChannelResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	if r.Body == nil {
		return nil, &notify.ValidationError{Msg: "missing request body"}
	}
	req := notify.UpdateRequest{Name: r.Body.Name, Enabled: r.Body.Enabled}
	if r.Body.Config != nil {
		c := fromChannelConfig(r.Body.Config)
		req.Config = &c
	}
	if r.Body.Secrets != nil {
		s := fromChannelSecrets(r.Body.Secrets)
		req.Secrets = &s
	}
	if r.Body.ClearSecrets != nil {
		for _, name := range *r.Body.ClearSecrets {
			req.ClearSecrets = append(req.ClearSecrets, string(name))
		}
	}
	item, err := h.Notify.Update(ctx, a, r.ChannelId, req)
	if err != nil {
		return nil, err
	}
	return gen.UpdateNotificationChannel200JSONResponse(toChannel(item)), nil
}

func (h *Handlers) DeleteNotificationChannel(ctx context.Context, r gen.DeleteNotificationChannelRequestObject) (gen.DeleteNotificationChannelResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	if err := h.Notify.Delete(ctx, a, r.ChannelId); err != nil {
		return nil, err
	}
	return gen.DeleteNotificationChannel204Response{}, nil
}
