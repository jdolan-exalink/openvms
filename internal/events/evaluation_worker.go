package events

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/jdolan-exalink/openvms/internal/authz"
	"github.com/jdolan-exalink/openvms/internal/rules"
	"github.com/jdolan-exalink/openvms/internal/store"
	"github.com/jdolan-exalink/openvms/internal/store/db"
)

const (
	ruleEvaluationBatchSize = 25
	ruleEvaluationLease     = 2 * time.Minute
	ruleEvaluationAttempts  = 5
	ruleEvaluationRetryBase = 2 * time.Second
	ruleEvaluationRetryMax  = 5 * time.Minute
	ruleEvaluationPoll      = time.Second
)

var errRuleEvaluationLeaseLost = errors.New("rule evaluation lease lost")

// RuleEvaluationWorker processes durable rule-evaluation inputs.
type RuleEvaluationWorker struct {
	Store *store.Store
	Rules *rules.Service
	Log   *slog.Logger
}

type claimedRuleEvaluation struct {
	job      db.RuleEvaluationJob
	tenantID uuid.UUID
}

func NewRuleEvaluationWorker(st *store.Store, evaluator *rules.Service, log *slog.Logger) *RuleEvaluationWorker {
	if log == nil {
		log = slog.Default()
	}
	return &RuleEvaluationWorker{Store: st, Rules: evaluator, Log: log}
}

// RunOnce claims a bounded batch and processes each input. Evaluation effects and completion
// share the tenant transaction.
func (w *RuleEvaluationWorker) RunOnce(ctx context.Context) error {
	if w == nil || w.Store == nil || w.Rules == nil {
		return errors.New("rule evaluation worker requires a store and evaluator")
	}
	claimed, err := w.claim(ctx)
	if err != nil {
		return err
	}
	var failures []error
	for _, item := range claimed {
		if err := w.process(ctx, item); err != nil {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}

// Run processes pending jobs immediately, then polls until ctx is cancelled.
func (w *RuleEvaluationWorker) Run(ctx context.Context) {
	if w == nil {
		return
	}
	ticker := time.NewTicker(ruleEvaluationPoll)
	defer ticker.Stop()
	for {
		if ctx.Err() != nil {
			return
		}
		if err := w.RunOnce(ctx); err != nil && ctx.Err() == nil {
			w.Log.WarnContext(ctx, "rule evaluation worker failed", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (w *RuleEvaluationWorker) claim(ctx context.Context) ([]claimedRuleEvaluation, error) {
	var claimed []claimedRuleEvaluation
	err := w.Store.TxRaw(ctx, store.AllTenants, func(tx pgx.Tx) error {
		jobs, err := db.New(tx).ClaimRuleEvaluationJobs(ctx, db.ClaimRuleEvaluationJobsParams{
			MaxRows: ruleEvaluationBatchSize, LeaseSeconds: ruleEvaluationLease.Seconds(), MaxAttempts: ruleEvaluationAttempts,
		})
		if err != nil {
			return err
		}
		claimed = make([]claimedRuleEvaluation, 0, len(jobs))
		for _, job := range jobs {
			var tenantID uuid.UUID
			if err := tx.QueryRow(ctx, `SELECT tenant_id FROM events WHERE id=$1`, job.EventID).Scan(&tenantID); err != nil {
				return fmt.Errorf("resolve tenant for evaluation job %s: %w", job.ID, err)
			}
			claimed = append(claimed, claimedRuleEvaluation{job: job, tenantID: tenantID})
		}
		return nil
	})
	return claimed, err
}

func (w *RuleEvaluationWorker) process(ctx context.Context, item claimedRuleEvaluation) error {
	job := item.job
	var ev rules.EventContext
	err := json.Unmarshal(job.Payload, &ev)
	if err == nil {
		switch {
		case ev.ID != job.EventID:
			err = errors.New("payload event id does not match job")
		case ev.TenantID != item.tenantID:
			err = errors.New("payload tenant does not match event tenant")
		case job.EvaluationKind == "face_update" && !ev.FaceUpdate:
			err = errors.New("face-update job payload is not marked as a face update")
		case job.EvaluationKind == "event" && ev.FaceUpdate:
			err = errors.New("event job payload is marked as a face update")
		case job.EvaluationKind != "event" && job.EvaluationKind != "face_update":
			err = fmt.Errorf("unknown evaluation kind %q", job.EvaluationKind)
		}
	}
	if err == nil {
		err = w.evaluateAndComplete(ctx, item, ev)
	}
	if err == nil {
		return nil
	}
	if errors.Is(err, errRuleEvaluationLeaseLost) {
		// A newer lease owns the row. The transaction above rolled back all effects; the old
		// token cannot retry, complete, or publish anything.
		return nil
	}
	w.Log.ErrorContext(ctx, "rule evaluation job failed", "job_id", job.ID, "tenant_id", item.tenantID,
		"attempt", job.Attempts, "error", err)
	if retryErr := w.retry(ctx, item, err); retryErr != nil {
		return fmt.Errorf("retry rule evaluation job %s after %w: %w", job.ID, err, retryErr)
	}
	return nil
}

func (w *RuleEvaluationWorker) evaluateAndComplete(ctx context.Context, item claimedRuleEvaluation, ev rules.EventContext) error {
	job := item.job
	if job.LeaseToken == nil {
		return errors.New("claimed rule evaluation job has no lease token")
	}
	actor := authz.Actor{TenantID: &item.tenantID}
	err := w.Store.TxRaw(ctx, store.ScopeFor(actor), func(tx pgx.Tx) error {
		var eventTenant uuid.UUID
		var eventPlates []string
		if err := tx.QueryRow(ctx, `SELECT tenant_id, coalesce(plates, '{}') FROM events WHERE id=$1`, job.EventID).Scan(&eventTenant, &eventPlates); err != nil {
			return fmt.Errorf("load tenant event %s: %w", job.EventID, err)
		}
		if eventTenant != item.tenantID || ev.TenantID != eventTenant {
			return errors.New("evaluation event tenant changed after claim")
		}
		if len(eventPlates) > 0 {
			ev.Plates = append(ev.Plates, eventPlates...)
		}
		var err error
		if err := w.Rules.EvaluateEventTx(ctx, tx, ev); err != nil {
			return err
		}
		rows, err := db.New(tx).CompleteRuleEvaluationJob(ctx, db.CompleteRuleEvaluationJobParams{
			ID: job.ID, LeaseToken: job.LeaseToken,
		})
		if err != nil {
			return err
		}
		if rows != 1 {
			return errRuleEvaluationLeaseLost
		}
		return nil
	})
	if err != nil {
		return err
	}
	return nil
}

func (w *RuleEvaluationWorker) retry(ctx context.Context, item claimedRuleEvaluation, cause error) error {
	job := item.job
	if job.LeaseToken == nil {
		return errors.New("cannot retry rule evaluation job without lease token")
	}
	lastError := cause.Error()
	if len(lastError) > 1024 {
		lastError = lastError[:1024]
	}
	seconds := ruleEvaluationRetryBase.Seconds()
	for attempt := int32(1); attempt < job.Attempts && seconds < ruleEvaluationRetryMax.Seconds(); attempt++ {
		seconds *= 2
	}
	if seconds > ruleEvaluationRetryMax.Seconds() {
		seconds = ruleEvaluationRetryMax.Seconds()
	}
	actor := authz.Actor{TenantID: &item.tenantID}
	return w.Store.Tx(ctx, store.ScopeFor(actor), func(q *db.Queries) error {
		rows, err := q.RetryRuleEvaluationJob(ctx, db.RetryRuleEvaluationJobParams{
			ID: job.ID, LeaseToken: job.LeaseToken, MaxAttempts: ruleEvaluationAttempts,
			RetrySeconds: seconds, LastError: &lastError,
		})
		if err != nil {
			return err
		}
		if rows == 0 {
			// The lease expired and another claimant rotated the token.
			w.Log.InfoContext(ctx, "skipped retry for a reclaimed rule evaluation job", "job_id", job.ID)
		}
		return nil
	})
}
