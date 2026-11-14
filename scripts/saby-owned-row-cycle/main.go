// This runner is built only against an explicitly pinned product-source archive.
// It never creates, posts, cancels or changes the header of a document.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/avpavlo8/ficusin-store/backend/internal/integration"
)

const nativeUUID = "8d6e3870-40b3-4c82-8d2b-ccefd310637d"
const publicUUID = "1eacfa69-a5be-4790-8b9a-a198b3493220"

var identity = integration.SabyOrderRealizationIdentity{ID: 42807, UUID: nativeUUID, WarehouseID: 283, MovementWarehouseID: 284}

type clientAPI interface {
	ReadOrderRealizationWithLines(context.Context, integration.SabyOrderRealizationIdentity) (integration.SabyOrderRealizationSnapshot, error)
	ReadOrderRealizationPricedLines(context.Context, integration.SabyOrderRealizationIdentity) ([]integration.SabyOrderRealizationPricedLine, error)
	DeleteOrderRealizationRow(context.Context, integration.SabyOrderRealizationDeleteRequest) (integration.SabyOrderRealizationDeleteResult, error)
	PrepareOrderRealizationRow(context.Context, integration.SabyOrderRealizationFillRequest) (integration.SabyOrderRealizationFillResult, error)
	SavePreparedOrderRealizationRow(context.Context, integration.SabyOrderRealizationFillRequest, integration.SabyOrderRealizationPreparedRow) (integration.SabyOrderRealizationFillResult, error)
}

type journal struct{ f *os.File }

func newJournal(path string) (*journal, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return nil, errors.New("journal_not_exclusive")
	}
	if err = f.Sync(); err != nil {
		f.Close()
		return nil, err
	}
	d, err := os.Open(filepath.Dir(path))
	if err != nil {
		f.Close()
		return nil, err
	}
	err = d.Sync()
	d.Close()
	if err != nil {
		f.Close()
		return nil, err
	}
	return &journal{f}, nil
}
func (j *journal) record(stage string, value any) error {
	if err := json.NewEncoder(j.f).Encode(map[string]any{"stage": stage, "at": time.Now().UTC(), "data": value}); err != nil {
		return errors.New("journal_write_failed")
	}
	if err := j.f.Sync(); err != nil {
		return errors.New("journal_sync_failed")
	}
	return nil
}

// Independent network allowlist: even a future source regression cannot invoke
// document creation, posting, cancellation, payment, stock or another service.
type scopedTransport struct {
	base   http.RoundTripper
	mu     sync.Mutex
	writes map[string]int
}

func (t *scopedTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.Method != "POST" || req.URL.Scheme != "https" || req.URL.Host != "online.sbis.ru" || req.URL.User != nil || req.URL.Fragment != "" {
		return nil, errors.New("network_scope_rejected")
	}
	if req.URL.Path == "/oauth/service/" && req.URL.RawQuery == "" {
		return t.base.RoundTrip(req)
	}
	if req.URL.Path != "/service/" || req.URL.RawQuery != "srv=1" {
		return nil, errors.New("network_path_rejected")
	}
	raw, err := io.ReadAll(io.LimitReader(req.Body, (8<<20)+1))
	req.Body.Close()
	if err != nil || len(raw) > 8<<20 {
		return nil, errors.New("rpc_body_rejected")
	}
	var body struct {
		Method string `json:"method"`
	}
	if json.Unmarshal(raw, &body) != nil {
		return nil, errors.New("rpc_invalid")
	}
	write := false
	switch body.Method {
	case "ДокОтгрИсх.Прочитать", "РеалИсх.DocNomList":
	case "РеалИсх.CustomDocNomDelete", "РеалИсх.DocNomCreate", "РеалИсх.CustomDocNomUpdate":
		write = true
	default:
		return nil, errors.New("rpc_method_rejected")
	}
	if write {
		t.mu.Lock()
		n := t.writes[body.Method]
		if n >= 2 {
			t.mu.Unlock()
			return nil, errors.New("write_limit_rejected")
		}
		t.writes[body.Method] = n + 1
		t.mu.Unlock()
	}
	req.Body = io.NopCloser(bytes.NewReader(raw))
	return t.base.RoundTrip(req)
}

func exact(n json.Number, want string) bool {
	a, ok := new(big.Rat).SetString(string(n))
	b, bok := new(big.Rat).SetString(want)
	return ok && bok && a.Cmp(b) == 0
}
func read(ctx context.Context, c clientAPI, price string) ([]integration.SabyOrderRealizationPricedLine, error) {
	s, err := c.ReadOrderRealizationWithLines(ctx, identity)
	if err != nil {
		return nil, errors.New("read_header_failed")
	}
	if s.ID != identity.ID || s.UUID != nativeUUID || s.PublicUUID != publicUUID || s.Type != 226 || s.TypeName != "ДокОтгрИсх" || s.WarehouseID != 283 || s.OrganizationID != 259 || s.RegulationID != 122498 || s.BuyerID != 18655 || !s.PostingStateKnown || s.Posted || s.HasEntries || s.PostingErrors || s.StaleEntries || s.PostingQueued || s.EntriesQueued || s.HeaderPosted {
		return nil, errors.New("fixed_unposted_header_mismatch")
	}
	lines, err := c.ReadOrderRealizationPricedLines(ctx, identity)
	if err != nil {
		return nil, errors.New("read_lines_failed")
	}
	if len(s.Lines) != len(lines) {
		return nil, errors.New("inconsistent_read")
	}
	if price == "" {
		if len(lines) != 0 {
			return nil, errors.New("expected_empty_document")
		}
		return lines, nil
	}
	if len(lines) != 1 {
		return nil, errors.New("expected_exact_one_line")
	}
	l := lines[0]
	if l.ID <= 0 || l.NomenclatureID != 3899 || l.DocumentID != 42807 || l.WarehouseID != 284 || l.Direction != 1 || l.Closed || !exact(l.Quantity, "1") || !exact(l.UnitPrice, price) || !exact(l.LineSum, price) {
		return nil, errors.New("fixed_line_mismatch")
	}
	h := s.Lines[0]
	if h.ID != l.ID || h.NomenclatureID != l.NomenclatureID || h.DocumentID != l.DocumentID || h.WarehouseID != l.WarehouseID || h.Direction != l.Direction || h.Closed || !exact(h.Quantity, "1") {
		return nil, errors.New("inconsistent_line_read")
	}
	return lines, nil
}

// A failed/unknown helper response ends all writes. Recovery is bounded RO only;
// in particular it never adopts a matching row as the acknowledgement of Save.
func recoverRead(ctx context.Context, c clientAPI, j *journal) {
	for range 2 {
		s, e := c.ReadOrderRealizationWithLines(ctx, identity)
		if e == nil {
			_ = j.record("read_only_recovery", map[string]any{"documentID": s.ID, "posted": s.Posted, "lineCount": len(s.Lines)})
		}
		if ctx.Err() != nil {
			return
		}
	}
}
func cycle(ctx context.Context, c clientAPI, j *journal) (err error) {
	defer func() {
		if err != nil {
			recoverRead(ctx, c, j)
		}
	}()
	rows, err := read(ctx, c, "1.00")
	if err != nil {
		return err
	}
	if err = j.record("baseline", rows); err != nil {
		return err
	}
	for index, price := range []string{"1.01", "1.00"} {
		oldPrice := "1.00"
		if index == 1 {
			oldPrice = "1.01"
		}
		rows, err = read(ctx, c, oldPrice)
		if err != nil {
			return err
		}
		if err = j.record("before_delete", rows); err != nil {
			return err
		}
		d, e := c.DeleteOrderRealizationRow(ctx, integration.SabyOrderRealizationDeleteRequest{Identity: identity, PublicUUID: publicUUID, OrganizationID: 259, RegulationID: 122498, BuyerID: 18655, ExpectedLines: rows, RowID: rows[0].ID})
		if err = j.record("delete_response", d); err != nil {
			return err
		}
		if e != nil || d.Outcome != "acknowledged" {
			return errors.New("delete_outcome_not_acknowledged")
		}
		if _, err = read(ctx, c, ""); err != nil {
			return err
		}
		next := integration.SabyOrderRealizationFillLine{NomenclatureID: 3899, Quantity: json.Number("1"), UnitPrice: json.Number(price)}
		request := integration.SabyOrderRealizationFillRequest{Identity: identity, OrganizationID: 259, RegulationID: 122498, BuyerID: 18655, Lines: []integration.SabyOrderRealizationFillLine{next}, Next: next}
		if err = j.record("before_prepare", request); err != nil {
			return err
		}
		prepared, e := c.PrepareOrderRealizationRow(ctx, request)
		if err = j.record("prepare_response", prepared); err != nil {
			return err
		}
		if e != nil || prepared.Outcome != "prepared" || prepared.Prepared == nil {
			return errors.New("prepare_outcome_not_acknowledged")
		}
		if _, err = read(ctx, c, ""); err != nil {
			return err
		}
		if err = j.record("before_save", prepared.Prepared); err != nil {
			return err
		}
		saved, e := c.SavePreparedOrderRealizationRow(ctx, request, *prepared.Prepared)
		if err = j.record("save_response", saved); err != nil {
			return err
		}
		if e != nil || saved.Outcome != "saved" || saved.RowID <= 0 {
			return errors.New("save_outcome_not_acknowledged")
		}
		rows, err = read(ctx, c, price)
		if err != nil {
			return err
		}
		if rows[0].ID != saved.RowID {
			return errors.New("save_readback_identity_mismatch")
		}
		if err = j.record("verified_unposted_fill", rows); err != nil {
			return err
		}
	}
	return j.record("complete", map[string]any{"documentID": 42807, "unposted": true, "quantity": "1", "unitPrice": "1.00", "postingRequested": false})
}
func main() {
	path := flag.String("journal", "", "exclusive durable journal path")
	flag.Parse()
	if *path == "" {
		os.Exit(2)
	}
	j, err := newJournal(*path)
	if err != nil {
		os.Exit(2)
	}
	defer j.f.Close()
	base := http.DefaultTransport.(*http.Transport).Clone()
	base.Proxy = nil
	http.DefaultTransport = &scopedTransport{base: base, writes: map[string]int{}}
	// Native helpers do not access point/pricelist endpoints. Required legacy
	// Configured() fields use existing service defaults; transport denies them.
	c := integration.NewSabyClient(os.Getenv("SABY_APP_CLIENT_ID"), os.Getenv("SABY_APP_SECRET"), os.Getenv("SABY_SECRET_KEY"), 278, 6)
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	err = cycle(ctx, c, j)
	if err != nil {
		_ = j.record("stopped", map[string]any{"readOnlyRecovery": true})
		fmt.Println(`{"ok":false,"reason":"owned_row_cycle_stopped","postingRequested":false,"retryPolicy":"read-only"}`)
		os.Exit(1)
	}
	fmt.Println(`{"ok":true,"documentID":42807,"finalUnposted":true,"finalQuantity":"1","finalPrice":"1.00","postingRequested":false}`)
}
