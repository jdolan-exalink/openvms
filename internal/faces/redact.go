package faces

import (
	"context"

	"github.com/google/uuid"

	"github.com/jdolan-exalink/openvms/internal/access"
	"github.com/jdolan-exalink/openvms/internal/authz"
	"github.com/jdolan-exalink/openvms/internal/store/db"
)

// Redactor hides recognized face names from callers without faces.view. Frigate puts the
// person's frigate_name into an event's sub_labels, so a sub_label that equals the
// frigate_name of a registered person of the event's own tenant is a face name; every other
// label (plates, free text) is left alone. Plate redaction (lpr.view) is separate.
type Redactor struct {
	hidden map[uuid.UUID]map[string]struct{}
}

// NewRedactor resolves which of the candidate labels are face names the actor may not see.
// One query per call, however many events are involved.
func NewRedactor(ctx context.Context, q *db.Queries, c *access.Checker, candidates ...[]string) (*Redactor, error) {
	r := &Redactor{}
	seen := map[string]struct{}{}
	var names []string
	for _, list := range candidates {
		for _, l := range list {
			if _, ok := seen[l]; !ok {
				seen[l] = struct{}{}
				names = append(names, l)
			}
		}
	}
	if len(names) == 0 {
		return r, nil
	}
	rows, err := q.ListFaceNamesIn(ctx, names)
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		if c.Can(authz.FacesView, access.Tenant(row.TenantID)) {
			continue
		}
		if r.hidden == nil {
			r.hidden = map[uuid.UUID]map[string]struct{}{}
		}
		if r.hidden[row.TenantID] == nil {
			r.hidden[row.TenantID] = map[string]struct{}{}
		}
		r.hidden[row.TenantID][row.FrigateName] = struct{}{}
	}
	return r, nil
}

// Active reports whether anything is hidden for this actor.
func (r *Redactor) Active() bool { return r != nil && len(r.hidden) > 0 }

// Strip returns labels without the face names hidden in the given tenant. The input slice is
// never modified; an unchanged list is returned as is.
func (r *Redactor) Strip(tenantID uuid.UUID, labels []string) []string {
	hidden := r.hiddenIn(tenantID)
	if len(hidden) == 0 {
		return labels
	}
	out := make([]string, 0, len(labels))
	for _, l := range labels {
		if _, ok := hidden[l]; !ok {
			out = append(out, l)
		}
	}
	return out
}

// DropHiddenFilterValues removes filter values that are hidden face names in any tenant, so a
// caller cannot probe names through a filter: such a value behaves like a label nobody has.
func (r *Redactor) DropHiddenFilterValues(values []string) []string {
	if !r.Active() {
		return values
	}
	out := make([]string, 0, len(values))
	for _, v := range values {
		hide := false
		for _, names := range r.hidden {
			if _, ok := names[v]; ok {
				hide = true
				break
			}
		}
		if !hide {
			out = append(out, v)
		}
	}
	return out
}

func (r *Redactor) hiddenIn(tenantID uuid.UUID) map[string]struct{} {
	if r == nil {
		return nil
	}
	return r.hidden[tenantID]
}
