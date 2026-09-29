package notify

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/jdolan-exalink/openvms/internal/authz"
	"github.com/jdolan-exalink/openvms/internal/store"
	"github.com/jdolan-exalink/openvms/internal/store/db"
)

// TestResult is the outcome of a test message to one destination.
type TestResult struct {
	Destination string
	OK          bool
	Error       string
}

// SendTest sends a test message straight to every destination of the channel, bypassing the
// outbox so the administrator sees the outcome immediately. It works on disabled channels too.
func (s *Service) SendTest(ctx context.Context, actor authz.Actor, id uuid.UUID) ([]TestResult, error) {
	var row db.NotificationChannel
	err := s.tx(ctx, actor, func(q *db.Queries) error {
		var err error
		if row, err = q.GetNotificationChannel(ctx, db.GetNotificationChannelParams{ID: id, TenantID: *actor.TenantID}); err != nil {
			return store.Classify(err)
		}
		return audit(ctx, q, actor, ActionChannelTested, id, map[string]any{"name": row.Name, "type": row.Type})
	})
	if err != nil {
		return nil, err
	}
	sec, err := s.openSecrets(row)
	if err != nil {
		return nil, err
	}
	sender, err := NewSender(Type(row.Type), decodeConfig(row), sec, s.Deps)
	if err != nil {
		return nil, &ValidationError{Msg: err.Error()}
	}
	msg := Message{
		ID: uuid.NewString(), Title: "Prueba de canal",
		Body:     fmt.Sprintf("Este es un mensaje de prueba de OpenVMS para el canal '%s'.", row.Name),
		Severity: "info", Test: true, OccurredAt: time.Now(),
	}
	dests := Destinations(Type(row.Type), decodeConfig(row))
	out := make([]TestResult, 0, len(dests))
	for _, d := range dests {
		sctx, cancel := context.WithTimeout(ctx, sendTimeout)
		err := sender.Send(sctx, msg, d)
		cancel()
		r := TestResult{Destination: d, OK: err == nil}
		if err != nil {
			r.Error = errText2(err)
		}
		out = append(out, r)
	}
	return out, nil
}

func errText2(err error) string {
	var perm *permanentError
	if errors.As(err, &perm) {
		return perm.Error()
	}
	return err.Error()
}

// ListDeliveries returns the most recent deliveries, optionally of one channel.
func (s *Service) ListDeliveries(ctx context.Context, actor authz.Actor, channelID *uuid.UUID, limit int) ([]Delivery, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	var out []Delivery
	err := s.tx(ctx, actor, func(q *db.Queries) error {
		rows, err := q.ListNotificationDeliveries(ctx, db.ListNotificationDeliveriesParams{
			TenantID: *actor.TenantID, ChannelID: channelID, MaxRows: int32(limit), //nolint:gosec // bounded above
		})
		if err != nil {
			return store.Classify(err)
		}
		out = make([]Delivery, 0, len(rows))
		for _, r := range rows {
			out = append(out, Delivery{
				ID: r.ID, ChannelID: r.ChannelID, ChannelName: r.ChannelName, ChannelType: r.ChannelType,
				RuleID: r.RuleID, NotificationID: r.NotificationID, Destination: r.Destination, Status: r.Status,
				Attempts: int(r.Attempts), LastError: r.LastError, NextAttemptAt: r.NextAttemptAt,
				CreatedAt: r.CreatedAt, SentAt: r.SentAt,
			})
		}
		return nil
	})
	return out, err
}
