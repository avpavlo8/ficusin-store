package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/avpavlo8/ficusin-store/backend/internal/integration"
)

type fakeClient struct {
	t       *testing.T
	journal string
	rows    []integration.SabyOrderRealizationPricedLine
	writes  []string
	reads   int
	unknown string
	id      int64
}

func (f *fakeClient) written(stage string) {
	f.t.Helper()
	raw, e := os.ReadFile(f.journal)
	if e != nil {
		f.t.Fatal(e)
	}
	var last struct {
		Stage string `json:"stage"`
	}
	for _, line := range splitLines(raw) {
		if len(line) > 0 && json.Unmarshal(line, &last) != nil {
			f.t.Fatal("bad journal")
		}
	}
	if last.Stage != "before_"+stage {
		f.t.Fatal("write before durable intent", stage, last.Stage)
	}
	f.writes = append(f.writes, stage)
}
func splitLines(raw []byte) [][]byte {
	var out [][]byte
	start := 0
	for i, b := range raw {
		if b == '\n' {
			out = append(out, raw[start:i])
			start = i + 1
		}
	}
	return out
}
func (f *fakeClient) ReadOrderRealizationWithLines(context.Context, integration.SabyOrderRealizationIdentity) (integration.SabyOrderRealizationSnapshot, error) {
	f.reads++
	s := integration.SabyOrderRealizationSnapshot{ID: 42807, UUID: nativeUUID, PublicUUID: publicUUID, Type: 226, TypeName: "ДокОтгрИсх", WarehouseID: 283, OrganizationID: 259, RegulationID: 122498, BuyerID: 18655, PostingStateKnown: true}
	for _, l := range f.rows {
		s.Lines = append(s.Lines, integration.SabyOrderRealizationLine{ID: l.ID, NomenclatureID: l.NomenclatureID, DocumentID: l.DocumentID, WarehouseID: l.WarehouseID, Direction: l.Direction, Quantity: l.Quantity, Closed: l.Closed})
	}
	return s, nil
}
func (f *fakeClient) ReadOrderRealizationPricedLines(context.Context, integration.SabyOrderRealizationIdentity) ([]integration.SabyOrderRealizationPricedLine, error) {
	return append([]integration.SabyOrderRealizationPricedLine{}, f.rows...), nil
}
func (f *fakeClient) DeleteOrderRealizationRow(_ context.Context, r integration.SabyOrderRealizationDeleteRequest) (integration.SabyOrderRealizationDeleteResult, error) {
	f.written("delete")
	if len(f.rows) != 1 || r.RowID != f.rows[0].ID {
		f.t.Fatal("stale or foreign row ID")
	}
	f.rows = nil
	if f.unknown == "delete" {
		return integration.SabyOrderRealizationDeleteResult{Outcome: "unknown"}, errors.New("lost")
	}
	return integration.SabyOrderRealizationDeleteResult{Outcome: "acknowledged"}, nil
}
func (f *fakeClient) PrepareOrderRealizationRow(_ context.Context, r integration.SabyOrderRealizationFillRequest) (integration.SabyOrderRealizationFillResult, error) {
	f.written("prepare")
	if len(f.rows) != 0 {
		f.t.Fatal("prepare nonempty")
	}
	if f.unknown == "prepare" {
		return integration.SabyOrderRealizationFillResult{Outcome: "unknown"}, errors.New("lost")
	}
	return integration.SabyOrderRealizationFillResult{Outcome: "prepared", Prepared: &integration.SabyOrderRealizationPreparedRow{Version: 1, Row: json.RawMessage(`{"fixed":true}`)}}, nil
}
func (f *fakeClient) SavePreparedOrderRealizationRow(_ context.Context, r integration.SabyOrderRealizationFillRequest, p integration.SabyOrderRealizationPreparedRow) (integration.SabyOrderRealizationFillResult, error) {
	f.written("save")
	if len(f.rows) != 0 || p.Version != 1 {
		f.t.Fatal("bad save")
	}
	f.id++
	f.rows = []integration.SabyOrderRealizationPricedLine{{ID: f.id, NomenclatureID: 3899, DocumentID: 42807, WarehouseID: 284, Direction: 1, Quantity: "1", UnitPrice: r.Next.UnitPrice, LineSum: r.Next.UnitPrice}}
	if f.unknown == "save" {
		return integration.SabyOrderRealizationFillResult{Outcome: "unknown"}, errors.New("lost ack")
	}
	return integration.SabyOrderRealizationFillResult{Outcome: "saved", RowID: f.id}, nil
}
func fixture(t *testing.T) (*fakeClient, *journal) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "journal.jsonl")
	j, e := newJournal(path)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { j.f.Close() })
	return &fakeClient{t: t, journal: path, id: 900, rows: []integration.SabyOrderRealizationPricedLine{{ID: 900, NomenclatureID: 3899, DocumentID: 42807, WarehouseID: 284, Direction: 1, Quantity: "1", UnitPrice: "1.00", LineSum: "1.00"}}}, j
}
func TestCycleUsesFreshIDsAndReturnsToUnpostedOneRuble(t *testing.T) {
	f, j := fixture(t)
	if e := cycle(context.Background(), f, j); e != nil {
		t.Fatal(e)
	}
	if len(f.writes) != 6 || len(f.rows) != 1 || f.rows[0].ID != 902 || !exact(f.rows[0].UnitPrice, "1") {
		t.Fatal(f.writes, f.rows)
	}
	if _, e := newJournal(f.journal); e == nil {
		t.Fatal("replayed journal permitted")
	}
}
func TestUnknownWriteCanOnlyReadAndStopsTheCycle(t *testing.T) {
	for _, stage := range []string{"delete", "prepare", "save"} {
		t.Run(stage, func(t *testing.T) {
			f, j := fixture(t)
			f.unknown = stage
			if e := cycle(context.Background(), f, j); e == nil {
				t.Fatal("unknown accepted")
			}
			want := map[string]int{"delete": 1, "prepare": 2, "save": 3}[stage]
			if len(f.writes) != want {
				t.Fatal("write replayed after unknown", f.writes)
			}
			if f.reads < 3 {
				t.Fatal("no bounded readback")
			}
		})
	}
}
func TestForeignBaselineCannotWrite(t *testing.T) {
	f, j := fixture(t)
	f.rows[0].NomenclatureID = 111
	if e := cycle(context.Background(), f, j); e == nil || len(f.writes) != 0 {
		t.Fatal("foreign row changed")
	}
}

type inertTransport struct{ calls int }

func (f *inertTransport) RoundTrip(*http.Request) (*http.Response, error) {
	f.calls++
	return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewBufferString(`{}`))}, nil
}
func TestTransportRejectsPostCreateCancelAndExtraWrites(t *testing.T) {
	base := &inertTransport{}
	tr := &scopedTransport{base: base, writes: map[string]int{}}
	for _, method := range []string{"Документ.PostAsync", "ДокОтгрИсх.Создать", "СБИС.ЗаписатьДокумент", "Документ.ОтменитьПроведение"} {
		raw, _ := json.Marshal(map[string]string{"method": method})
		r, _ := http.NewRequest("POST", "https://online.sbis.ru/service/?srv=1", bytes.NewReader(raw))
		if _, e := tr.RoundTrip(r); e == nil {
			t.Fatal("forbidden method accepted", method)
		}
	}
	if base.calls != 0 {
		t.Fatal("forbidden request reached network")
	}
	for i := range 3 {
		raw, _ := json.Marshal(map[string]string{"method": "РеалИсх.CustomDocNomDelete"})
		r, _ := http.NewRequest("POST", "https://online.sbis.ru/service/?srv=1", bytes.NewReader(raw))
		_, e := tr.RoundTrip(r)
		if (i < 2) != (e == nil) {
			t.Fatal("write bound", i, e)
		}
	}
	if base.calls != 2 {
		t.Fatal("write bound reached network", base.calls)
	}
}
func TestClosedJournalPreventsMutation(t *testing.T) {
	f, j := fixture(t)
	j.f.Close()
	if e := cycle(context.Background(), f, j); e == nil || len(f.writes) != 0 {
		t.Fatal("unsaved intent allowed a write")
	}
}
