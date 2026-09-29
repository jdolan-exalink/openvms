package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/jdolan-exalink/openvms/internal/store/db"
)

// revokableDB answers credential lookups with an empty row until revoked, then ErrNoRows.
type revokableDB struct {
	revoked bool
	lookups int
}

func (d *revokableDB) Exec(context.Context, string, ...interface{}) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, nil
}

func (d *revokableDB) Query(context.Context, string, ...interface{}) (pgx.Rows, error) {
	return nil, pgx.ErrNoRows
}

func (d *revokableDB) QueryRow(context.Context, string, ...interface{}) pgx.Row {
	d.lookups++
	return fakeRow{revoked: d.revoked}
}

type fakeRow struct{ revoked bool }

func (r fakeRow) Scan(...any) error {
	if r.revoked {
		return pgx.ErrNoRows
	}
	return nil
}

func TestAuthenticateInstallsRevalidatorThatSeesRevocation(t *testing.T) {
	for _, tc := range []struct {
		name  string
		setup func(r *http.Request)
	}{
		{"bearer token", func(r *http.Request) { r.Header.Set("Authorization", "Bearer tok") }},
		{"session cookie", func(r *http.Request) {
			r.AddCookie(&http.Cookie{Name: SessionCookie, Value: "sess", Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode})
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fake := &revokableDB{}
			var check func(context.Context) (bool, error)
			h := Authenticate(db.New(fake), AuthOptions{})(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
				check = RevalidatorFrom(r.Context())
			}))
			req := httptest.NewRequest(http.MethodGet, "/ws", nil)
			tc.setup(req)
			h.ServeHTTP(httptest.NewRecorder(), req)

			if check == nil {
				t.Fatal("Authenticate did not install a Revalidator for the request")
			}
			if ok, err := check(context.Background()); !ok || err != nil {
				t.Fatalf("live credential: got (%v, %v), want (true, nil)", ok, err)
			}
			fake.revoked = true
			before := fake.lookups
			if ok, err := check(context.Background()); ok || err != nil {
				t.Fatalf("revoked credential: got (%v, %v), want (false, nil)", ok, err)
			}
			if fake.lookups != before+1 {
				t.Fatalf("revalidation lookups = %d, want %d", fake.lookups-before, 1)
			}
		})
	}
}
