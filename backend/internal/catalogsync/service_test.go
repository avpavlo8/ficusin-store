package catalogsync

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"
)

type fakeStore struct {
	command        *Command
	sentErr        error
	sent, finished int
	state          string
}

func (f *fakeStore) View(context.Context, int64) (View, error) { return View{}, nil }
func (f *fakeStore) Enqueue(context.Context, int64, int64, string, bool) (View, error) {
	return View{}, nil
}
func (f *fakeStore) Claim(context.Context) (*Command, error) { return f.command, nil }
func (f *fakeStore) Sent(_ context.Context, c *Command) error {
	if f.sentErr != nil {
		return f.sentErr
	}
	f.sent++
	now := time.Now()
	c.AttemptedAt = &now
	return nil
}
func (f *fakeStore) Finish(context.Context, *Command, *Remote) error { f.finished++; return nil }
func (f *fakeStore) Release(_ context.Context, _ *Command, state, _ string, _ time.Duration) error {
	f.state = state
	return nil
}

type fakeProvider struct {
	remote   *Remote
	writeErr error
	writes   int
	price    bool
}

func (f *fakeProvider) Configured() bool                                  { return true }
func (f *fakeProvider) Lookup(context.Context, Snapshot) (*Remote, error) { return f.remote, nil }
func (f *fakeProvider) Write(_ context.Context, _ Snapshot, price bool) error {
	f.writes++
	f.price = price
	return f.writeErr
}
func commandFixture() *Command {
	return &Command{Snapshot: Snapshot{VariantID: 8, ProductID: 9, SKU: "8", Name: "Калатея D12", PotCM: 12, HeightCM: 40, PriceMinor: 149000, Code: "uuid"}, Revision: 1}
}
func quietLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }
func TestUnknownWriteNeverReposts(t *testing.T) {
	c := commandFixture()
	store := &fakeStore{command: c}
	p := &fakeProvider{writeErr: errors.New("timeout after accepted")}
	s := New(store, p, quietLogger())
	s.step(context.Background())
	if p.writes != 1 || store.state != "unknown" || c.AttemptedAt == nil {
		t.Fatal("unknown result not durable")
	}
	s = New(store, p, quietLogger())
	s.step(context.Background())
	if p.writes != 1 || store.sent != 1 {
		t.Fatal("POST repeated after restart")
	}
	p.remote = &Remote{ID: "71", Code: "uuid", Name: c.Snapshot.Name, PotCM: 12, HeightCM: 40, PriceMinor: 149000}
	s.step(context.Background())
	if store.finished != 1 || p.writes != 1 {
		t.Fatal("GET reconciliation failed")
	}
}
func TestSkipUnchangedPriceAndRejectStaleSnapshot(t *testing.T) {
	for _, stale := range []bool{false, true} {
		c := commandFixture()
		c.Snapshot.SabyID = "71"
		store := &fakeStore{command: c}
		if stale {
			store.sentErr = ErrConflict
		}
		p := &fakeProvider{remote: &Remote{ID: "71", Code: "uuid", Name: c.Snapshot.Name, PotCM: 12, HeightCM: 30, PriceMinor: 149000}}
		New(store, p, quietLogger()).step(context.Background())
		if stale {
			if p.writes != 0 || store.state != "error" {
				t.Fatal("stale data sent")
			}
		} else if p.writes != 1 || p.price {
			t.Fatal("unchanged price sent")
		}
	}
}
func TestMissingLinkedCardNeverCreates(t *testing.T) {
	c := commandFixture()
	c.Snapshot.SabyID = "71"
	store := &fakeStore{command: c}
	p := &fakeProvider{}
	New(store, p, quietLogger()).step(context.Background())
	if p.writes != 0 || store.state != "error" {
		t.Fatal("missing linked card recreated")
	}
}
func TestFingerprintSurvivesLinking(t *testing.T) {
	s := commandFixture().Snapshot
	h := s.Hash()
	s.SabyID = "71"
	s.LookupName = s.Name
	s.Article = "8"
	if h != s.Hash() {
		t.Fatal("linking changed fingerprint")
	}
	s.HeightCM++
	if h == s.Hash() {
		t.Fatal("measurements not hashed")
	}
}
