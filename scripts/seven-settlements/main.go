// One-time owner-approved scope; never merge this operational command into the application.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"math/big"
	"net/http"
	"net/url"
	"os"
	"reflect"
	"strings"
	"time"

	"github.com/avpavlo8/ficusin-store/backend/internal/config"
	"github.com/avpavlo8/ficusin-store/backend/internal/integration"
	"github.com/avpavlo8/ficusin-store/backend/internal/money"
	"github.com/avpavlo8/ficusin-store/backend/internal/payment"
	"github.com/avpavlo8/ficusin-store/backend/internal/store"
)

type approved struct {
	Order, Payment, Minor int64
	Number, Provider      string
}

var scope = []approved{
	{54, 35, 462000, "0000-7", "326d7ca5-000f-5001-a000-18d6ac6e21fc"},
	{55, 36, 486500, "0000-8", "3277a9cc-000f-5001-8000-1a04bbac6adf"},
	{57, 38, 87500, "0000-9", "327bc28c-000f-5001-8000-11c4b78cc20f"},
	{59, 40, 447500, "0000-11", "327d11af-000f-5001-a000-14bc5dbb1948"},
	{60, 41, 3035100, "0000-12", "327fbf49-000f-5001-9000-1a5b615dd159"},
	{61, 43, 627900, "0000-13", "32803316-000f-5001-8000-140dba0a310d"},
	{63, 44, 558100, "0015-1", "328397e6-000f-5000-b000-1cbd05c9cbee"},
}

type amount struct{ Value, Currency string }

// YooKassa returns numeric quantities while older requests used strings.
// Keep the exact decimal representation; never round fiscal quantities.
type decimal string

func (d *decimal) UnmarshalJSON(raw []byte) error {
	var value string
	if len(raw) > 0 && raw[0] == '"' {
		if err := json.Unmarshal(raw, &value); err != nil {
			return err
		}
	} else {
		var number json.Number
		if err := json.Unmarshal(raw, &number); err != nil {
			return err
		}
		value = number.String()
	}
	if _, ok := new(big.Rat).SetString(value); !ok {
		return errors.New("invalid_decimal_quantity")
	}
	*d = decimal(value)
	return nil
}

type item struct {
	Description string
	Quantity    decimal
	Amount      amount
	VAT         int    `json:"vat_code"`
	Mode        string `json:"payment_mode"`
	Subject     string `json:"payment_subject"`
	Measure     string `json:"measure"`
}
type receipt struct {
	ID           string `json:"id"`
	PaymentID    string `json:"payment_id"`
	Type, Status string
	Items        []item
	Settlements  []struct {
		Type   string
		Amount amount
	}
	FiscalDocument  string `json:"fiscal_document_number"`
	FiscalStorage   string `json:"fiscal_storage_number"`
	FiscalAttribute string `json:"fiscal_attribute"`
	RegisteredAt    string `json:"registered_at"`
}
type guardedProvider struct {
	*integration.YooKassaClient
	shop, secret   string
	http           *http.Client
	allowed        map[string]approved
	posts, adopted int
}

func equalDecimal(a, b string) bool {
	x, ok := new(big.Rat).SetString(a)
	if !ok {
		return false
	}
	y, ok := new(big.Rat).SetString(b)
	return ok && x.Cmp(y) == 0
}
func matchingItems(a, b []item, mode string) bool {
	if len(a) == 0 || len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Description != b[i].Description || a[i].VAT != b[i].VAT || a[i].Subject != b[i].Subject || a[i].Mode != mode || b[i].Mode != "full_payment" || a[i].Amount.Currency != "RUB" || b[i].Amount.Currency != "RUB" || !equalDecimal(a[i].Amount.Value, b[i].Amount.Value) || !equalDecimal(string(a[i].Quantity), string(b[i].Quantity)) || (a[i].Measure != "" && a[i].Measure != b[i].Measure) {
			return false
		}
	}
	return true
}
func (p *guardedProvider) get(ctx context.Context, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.yookassa.ru/v3"+path, nil)
	if err != nil {
		return errors.New("provider_request_invalid")
	}
	req.SetBasicAuth(p.shop, p.secret)
	req.Header.Set("Accept", "application/json")
	response, err := p.http.Do(req)
	if err != nil {
		return errors.New("provider_get_unavailable")
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		return fmt.Errorf("provider_get_http_%d", response.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, (4<<20)+1))
	if err != nil || len(raw) > 4<<20 {
		return errors.New("provider_response_invalid")
	}
	if json.Unmarshal(raw, out) != nil {
		return errors.New("provider_json_invalid")
	}
	return nil
}
func (p *guardedProvider) list(ctx context.Context, id string) ([]receipt, error) {
	all := []receipt{}
	cursor := ""
	seen := map[string]bool{}
	for range 3 {
		q := url.Values{"payment_id": {id}, "limit": {"100"}}
		if cursor != "" {
			q.Set("cursor", cursor)
		}
		var page struct {
			Items []receipt
			Next  string `json:"next_cursor"`
		}
		if err := p.get(ctx, "/receipts?"+q.Encode(), &page); err != nil {
			return nil, err
		}
		for _, r := range page.Items {
			if r.PaymentID != id || r.ID == "" || seen[r.ID] {
				return nil, errors.New("receipt_list_identity_mismatch")
			}
			seen[r.ID] = true
			all = append(all, r)
		}
		if page.Next == "" {
			return all, nil
		}
		if page.Next == cursor {
			return nil, errors.New("receipt_cursor_repeated")
		}
		cursor = page.Next
	}
	return nil, errors.New("receipt_list_incomplete")
}
func checkBody(req integration.SettlementReceiptRequest, a approved) ([]item, error) {
	if req.PaymentID != a.Provider || req.Body["payment_id"] != a.Provider || req.Body["type"] != "payment" || req.Body["send"] != true || req.Key == "" {
		return nil, errors.New("receipt_request_outside_scope")
	}
	raw, _ := json.Marshal(req.Body)
	var body struct {
		Items       []item
		Settlements []struct {
			Type   string
			Amount amount
		}
		Customer map[string]string
	}
	if json.Unmarshal(raw, &body) != nil || body.Customer["email"] == "" || len(body.Settlements) != 1 || body.Settlements[0].Type != "prepayment" || body.Settlements[0].Amount.Currency != "RUB" {
		return nil, errors.New("receipt_request_invalid")
	}
	total, err := money.Parse(body.Settlements[0].Amount.Value)
	if err != nil || total != a.Minor {
		return nil, errors.New("receipt_settlement_amount_mismatch")
	}
	sum := int64(0)
	for _, it := range body.Items {
		unit, e := money.Parse(it.Amount.Value)
		q, ok := new(big.Rat).SetString(string(it.Quantity))
		if e != nil || unit <= 0 || !ok || !q.IsInt() || !q.Num().IsInt64() || q.Num().Int64() <= 0 || q.Num().Int64() > 10000 || it.Amount.Currency != "RUB" || it.VAT <= 0 || it.Mode != "full_payment" || it.Subject == "" {
			return nil, errors.New("receipt_item_invalid")
		}
		sum += unit * q.Num().Int64()
	}
	if sum != a.Minor || len(body.Items) == 0 {
		return nil, errors.New("receipt_items_amount_mismatch")
	}
	return body.Items, nil
}

func (p *guardedProvider) PrepareSettlementReceipt(id, key string, original integration.PaymentRequest, items []integration.PaymentItem) (integration.SettlementReceiptRequest, error) {
	req, err := p.YooKassaClient.PrepareSettlementReceipt(id, key, original, items)
	if err != nil {
		return req, err
	}
	// These historical snapshots use positive integer counts for goods and
	// delivery. Explicitly preserve that unit in the durable request (FFD 1.2).
	for _, line := range req.Body["items"].([]map[string]any) {
		line["measure"] = "piece"
	}
	return req, nil
}
func (p *guardedProvider) verify(ctx context.Context, req integration.SettlementReceiptRequest) (*receipt, error) {
	a, ok := p.allowed[req.PaymentID]
	if !ok {
		return nil, errors.New("payment_not_approved")
	}
	items, err := checkBody(req, a)
	if err != nil {
		return nil, err
	}
	paid, err := p.FetchPayment(ctx, a.Provider)
	if err != nil {
		return nil, errors.New("payment_get_failed")
	}
	n, e := money.Exact(paid.Amount)
	if paid.ID != a.Provider || paid.Test == nil || *paid.Test || !paid.Paid || paid.Status != "succeeded" || paid.ReceiptStatus != "succeeded" || e != nil || n != a.Minor {
		return nil, errors.New("real_prepayment_not_confirmed")
	}
	receipts, err := p.list(ctx, a.Provider)
	if err != nil {
		return nil, err
	}
	prepayments := 0
	var final *receipt
	for _, r := range receipts {
		if r.Type != "payment" {
			return nil, errors.New("unexpected_refund_receipt")
		}
		if matchingItems(r.Items, items, "full_prepayment") && r.Status == "succeeded" {
			prepayments++
			continue
		}
		if matchingItems(r.Items, items, "full_payment") && (r.Status == "succeeded" || r.Status == "pending") && len(r.Settlements) == 1 && r.Settlements[0].Type == "prepayment" && r.Settlements[0].Amount.Currency == "RUB" {
			v, e := money.Parse(r.Settlements[0].Amount.Value)
			if e == nil && v == a.Minor && final == nil {
				copy := r
				final = &copy
				continue
			}
		}
		return nil, errors.New("unexpected_existing_receipt_needs_review")
	}
	if prepayments != 1 {
		return nil, errors.New("expected_one_registered_prepayment")
	}
	return final, nil
}
func (p *guardedProvider) CreateSettlementReceipt(ctx context.Context, req integration.SettlementReceiptRequest) (integration.FiscalReceipt, error) {
	final, err := p.verify(ctx, req)
	if err != nil {
		return integration.FiscalReceipt{}, err
	}
	if final != nil {
		p.adopted++
		return integration.FiscalReceipt{ID: final.ID, PaymentID: final.PaymentID, Type: final.Type, Status: final.Status}, nil
	}
	if p.posts >= len(scope) {
		return integration.FiscalReceipt{}, errors.New("approved_post_limit")
	}
	p.posts++
	return p.YooKassaClient.CreateSettlementReceipt(ctx, req)
}

func confirmKnownReceipt(r receipt, id string, req integration.SettlementReceiptRequest, a approved) bool {
	want, err := checkBody(req, a)
	if err != nil {
		return false
	}
	if r.ID != id || r.PaymentID != a.Provider || r.Type != "payment" || r.Status != "succeeded" || !matchingItems(r.Items, want, "full_payment") || len(r.Settlements) != 1 || r.Settlements[0].Type != "prepayment" || r.Settlements[0].Amount.Currency != "RUB" {
		return false
	}
	n, err := money.Parse(r.Settlements[0].Amount.Value)
	return err == nil && n == a.Minor
}

type settings struct {
	DatabaseURL, CA, Shop, Secret string
	Tax, VAT                      int
	VerifyTLS                     bool
}

func run() error {
	apply := flag.Bool("apply", false, "Create only the seven owner-approved settlements")
	flag.Parse()
	if os.Getenv("GITHUB_ACTIONS") != "true" || os.Getenv("APPROVED_SUPPORT_TICKET") != "31489326" {
		return errors.New("approved_workflow_only")
	}
	var env settings
	if json.NewDecoder(io.LimitReader(os.Stdin, 1<<20)).Decode(&env) != nil || env.Shop != "1420523" || env.Secret == "" {
		return errors.New("invalid_private_configuration")
	}
	u, err := url.Parse(env.DatabaseURL)
	if err != nil || u.Path != "/default_db" || u.User == nil {
		return errors.New("wrong_database_url")
	}
	login := u.User.Username()
	q := u.Query()
	readonly := "on"
	if *apply {
		readonly = "off"
	}
	q.Set("options", "-c statement_timeout=15000 -c lock_timeout=5000 -c default_transaction_read_only="+readonly)
	u.RawQuery = q.Encode()
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
	defer cancel()
	pool, err := store.Open(ctx, config.Database{URL: u.String(), CA: env.CA, VerifyTLS: env.VerifyTLS, MaxConnections: 4})
	if err != nil {
		return errors.New("verified_database_connection_failed")
	}
	defer pool.Close()
	var db, user, ro string
	if pool.QueryRow(ctx, "SELECT current_database(),current_user,current_setting('transaction_read_only')").Scan(&db, &user, &ro) != nil || db != "default_db" || user != login || ro != readonly {
		return errors.New("wrong_database_identity_or_mode")
	}
	var actor int64
	// Ownership comes from admin_users, never a mutable profile/email match.
	// The operation is explicitly authorized by this shop's owner. Refuse an
	// ambiguous actor instead of guessing among multiple active owners.
	if err := pool.QueryRow(ctx, `SELECT a.customer_id FROM admin_users a JOIN customers c ON c.id=a.customer_id WHERE a.role='owner' AND a.is_active AND (SELECT COUNT(*) FROM admin_users WHERE role='owner' AND is_active AND customer_id IS NOT NULL)=1`).Scan(&actor); err != nil || actor <= 0 {
		return errors.New("approved_owner_not_found")
	}
	client := integration.NewYooKassaClient(env.Shop, env.Secret, env.Tax, env.VAT)
	p := &guardedProvider{YooKassaClient: client, shop: env.Shop, secret: env.Secret, http: &http.Client{Timeout: 20 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("redirect_not_allowed") }}, allowed: map[string]approved{}}
	for _, a := range scope {
		p.allowed[a.Provider] = a
	}
	prepared := map[int64]integration.SettlementReceiptRequest{}
	originalHashes := map[int64]string{}
	for _, a := range scope {
		var number, providerID, status, paymentStatus, receiptStatus, mode, hash string
		var amount, total, paid int64
		var raw []byte
		var refunds int
		err := pool.QueryRow(ctx, `SELECT o.order_number,p.provider_payment_id,o.status,p.status,p.receipt_status,payment_fiscal_mode(p.request_payload),ROUND(p.amount*100)::BIGINT,ROUND(o.total*100)::BIGINT,p.request_payload,(SELECT COUNT(*) FROM payment_refunds WHERE order_id=o.id),ROUND(COALESCE((SELECT SUM(amount) FROM payments WHERE order_id=o.id AND status='paid'),0)*100)::BIGINT,encode(sha256(convert_to(p.request_payload::TEXT,'UTF8')),'hex') FROM payments p JOIN orders o ON o.id=p.order_id WHERE p.id=$1 AND p.order_id=$2 AND p.shipment_offer_id IS NULL`, a.Payment, a.Order).Scan(&number, &providerID, &status, &paymentStatus, &receiptStatus, &mode, &amount, &total, &raw, &refunds, &paid, &hash)
		if err != nil || number != a.Number || providerID != a.Provider || status != "completed" || paymentStatus != "paid" || receiptStatus != "succeeded" || mode != "full_prepayment" || amount != a.Minor || total != a.Minor || paid != a.Minor || refunds != 0 {
			return fmt.Errorf("order_%s_preconditions_failed", a.Number)
		}
		var original integration.PaymentRequest
		if json.Unmarshal(raw, &original) != nil {
			return errors.New("original_payment_snapshot_invalid")
		}
		req, err := p.PrepareSettlementReceipt(a.Provider, "review-only-not-sent", original, original.Items)
		if err != nil {
			return errors.New("canonical_receipt_prepare_failed")
		}
		if _, err = p.verify(ctx, req); err != nil {
			return fmt.Errorf("order_%s_%w", a.Number, err)
		}
		prepared[a.Payment] = req
		originalHashes[a.Payment] = hash
	}
	fmt.Printf("{\"phase\":\"prepared\",\"dryRun\":%t,\"orders\":7,\"totalMinor\":5704600,\"scopeAndOriginalPrepaymentsVerified\":true,\"supportTicket\":31489326}\n", !*apply)
	if !*apply {
		return nil
	}
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	service := payment.NewService(pool, p, "https://ficusin.ru", logger)
	for _, a := range scope {
		if err := service.SettlePaymentReceipt(ctx, a.Order, a.Payment, actor); err != nil {
			return fmt.Errorf("order_%s_canonical_service_failed", a.Number)
		}
		var status, id string
		for attempt := 0; attempt < 12; attempt++ {
			if err := pool.QueryRow(ctx, `SELECT status,provider_receipt_id FROM payment_settlement_receipts WHERE payment_id=$1`, a.Payment).Scan(&status, &id); err != nil {
				return errors.New("receipt_journal_read_failed")
			}
			if status == "succeeded" {
				break
			}
			if status != "pending" || id == "" {
				return errors.New("receipt_requires_review")
			}
			time.Sleep(5 * time.Second)
			if err := service.SettlePaymentReceipt(ctx, a.Order, a.Payment, actor); err != nil {
				return errors.New("known_receipt_status_sync_failed")
			}
		}
		if status != "succeeded" {
			return errors.New("receipt_registration_pending")
		}
		// The known-ID endpoint is authoritative; the list may lag immediately
		// after registration. This is GET only and never retries a fiscal POST.
		var final receipt
		err := p.get(ctx, "/receipts/"+url.PathEscape(id), &final)
		if err != nil || !confirmKnownReceipt(final, id, prepared[a.Payment], a) {
			return errors.New("registered_receipt_not_confirmed_by_get")
		}
		var body []byte
		var savedActor int64
		var source, hash string
		if err := pool.QueryRow(ctx, `SELECT r.request_payload,r.actor_id,r.source,encode(sha256(convert_to(p.request_payload::TEXT,'UTF8')),'hex') FROM payment_settlement_receipts r JOIN payments p ON p.id=r.payment_id WHERE r.payment_id=$1`, a.Payment).Scan(&body, &savedActor, &source, &hash); err != nil || savedActor != actor || source != "owner" || hash != originalHashes[a.Payment] {
			return errors.New("receipt_audit_or_original_snapshot_changed")
		}
		var saved integration.SettlementReceiptRequest
		if json.Unmarshal(body, &saved) != nil || !reflect.DeepEqual(saved.Body, jsonBody(prepared[a.Payment].Body)) {
			return errors.New("frozen_receipt_body_mismatch")
		}
		raw, _ := json.Marshal(map[string]any{"phase": "registered", "order": a.Number, "paymentId": a.Payment, "amountMinor": a.Minor, "receiptId": id, "status": status, "fiscalDocument": final.FiscalDocument, "fiscalStorage": final.FiscalStorage, "fiscalAttribute": final.FiscalAttribute, "registeredAt": final.RegisteredAt, "originalSnapshotUnchanged": true})
		fmt.Println(string(raw))
	}
	fmt.Printf("{\"ok\":true,\"registered\":7,\"providerPosts\":%d,\"adoptedExisting\":%d,\"supportTicket\":31489326}\n", p.posts, p.adopted)
	return nil
}
func jsonBody(body map[string]any) map[string]any {
	raw, _ := json.Marshal(body)
	var out map[string]any
	_ = json.Unmarshal(raw, &out)
	return out
}
func main() {
	if err := run(); err != nil {
		code := err.Error()
		if strings.ContainsAny(code, "\n\r") || len(code) > 180 {
			code = "operation_failed"
		}
		raw, _ := json.Marshal(map[string]any{"ok": false, "reason": code})
		fmt.Println(string(raw))
		os.Exit(1)
	}
}
