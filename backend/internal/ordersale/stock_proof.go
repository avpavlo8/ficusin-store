package ordersale

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"regexp"
	"strconv"
	"strings"
)

// StockProof is the versioned readback receipt, not a provider's success flag.
// A live provider must fill it from verified native header and movement rows.
// Every field is serialized, including false flags: missing means unknown.
type StockProof struct {
	Version             int              `json:"version"`
	Test                bool             `json:"test"`
	ExecutionMode       string           `json:"executionMode"`
	IntentID            int64            `json:"intentId"`
	EventKey            string           `json:"eventKey"`
	EventFingerprint    string           `json:"eventFingerprint"`
	OperationKey        string           `json:"operationKey"`
	DocumentID          string           `json:"documentId"`
	DocumentUUID        string           `json:"documentUUID"`
	SiteWarehouseID     int64            `json:"siteWarehouseId"`
	NativeWarehouseID   int64            `json:"nativeWarehouseId"`
	MovementWarehouseID int64            `json:"movementWarehouseId"`
	OrganizationID      int64            `json:"organizationId"`
	RegulationID        int64            `json:"regulationId"`
	BuyerID             int64            `json:"buyerId"`
	Posted              bool             `json:"posted"`
	HasEntries          bool             `json:"hasEntries"`
	PostingErrors       bool             `json:"postingErrors"`
	StaleEntries        bool             `json:"staleEntries"`
	PostingQueued       bool             `json:"postingQueued"`
	EntriesQueued       bool             `json:"entriesQueued"`
	Lines               []StockProofLine `json:"lines"`
}

type StockProofLine struct {
	ID             int64       `json:"id"`
	DocumentID     string      `json:"documentId"`
	NomenclatureID string      `json:"nomenclatureId"`
	WarehouseID    int64       `json:"warehouseId"`
	Direction      int64       `json:"direction"`
	Quantity       json.Number `json:"quantity"`
	UnitPrice      json.Number `json:"unitPrice"`
	LineSum        json.Number `json:"lineSum"`
	Closed         bool        `json:"closed"`
}

const maxStockProofBytes = 1 << 20
const maxStockProofLines = 4096

var stockProofUUID = regexp.MustCompile(`(?i)^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
var stockProofFingerprint = regexp.MustCompile(`^[0-9a-f]{64}$`)
var stockProofQuantity = regexp.MustCompile(`^[0-9]{1,18}(?:\.[0-9]{1,9})?(?:[eE][+-]?[0-9]{1,2})?$`)
var stockProofMoney = regexp.MustCompile(`^(?:0|[1-9][0-9]{0,17})(?:\.[0-9]{1,2})?$`)

func validStockProof(request Request, result Result) bool {
	if result.State != "posted" || !result.StockVerified || result.Progress.Unknown || result.Progress.IdentityConflict || len(result.StockProof) == 0 || len(result.StockProof) > maxStockProofBytes {
		return false
	}
	if request.ExecutionMode != "mock" && request.ExecutionMode != "live" {
		return false
	}
	if request.IntentID <= 0 || request.Event.OrderID <= 0 || request.Event.ShipmentOfferID < 0 || request.SiteWarehouseID <= 0 || !request.Event.CanPrepare ||
		request.Event.Key != eventKey(request.Event.OrderID, request.Event.ShipmentOfferID) || !stockProofFingerprint.MatchString(request.Event.Fingerprint) || !stockProofUUID.MatchString(request.OperationKey) {
		return false
	}
	var proof StockProof
	decoder := json.NewDecoder(bytes.NewReader(result.StockProof))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&proof) != nil || decoder.Decode(new(any)) != io.EOF ||
		!completeProofObject(result.StockProof, "version", "test", "executionMode", "intentId", "eventKey", "eventFingerprint", "operationKey", "documentId", "documentUUID", "siteWarehouseId", "nativeWarehouseId", "movementWarehouseId", "organizationId", "regulationId", "buyerId", "posted", "hasEntries", "postingErrors", "staleEntries", "postingQueued", "entriesQueued", "lines") {
		return false
	}
	if proof.Version != 1 || proof.ExecutionMode != request.ExecutionMode || proof.IntentID != request.IntentID || proof.EventKey != request.Event.Key || proof.EventFingerprint != request.Event.Fingerprint ||
		!strings.EqualFold(proof.OperationKey, request.OperationKey) || proof.DocumentID != result.Progress.DocumentID || !stockProofUUID.MatchString(proof.DocumentUUID) || !strings.EqualFold(proof.DocumentUUID, result.Progress.DocumentUUID) ||
		proof.SiteWarehouseID != request.SiteWarehouseID || proof.NativeWarehouseID != request.NativeWarehouseID || proof.MovementWarehouseID != request.MovementWarehouseID || proof.OrganizationID != request.OrganizationID || proof.RegulationID != request.RegulationID || proof.BuyerID != request.BuyerID ||
		!proof.Posted || !proof.HasEntries || proof.PostingErrors || proof.StaleEntries || proof.PostingQueued || proof.EntriesQueued {
		return false
	}
	if request.ExecutionMode == "mock" {
		if !proof.Test || proof.DocumentID != fmt.Sprintf("STG-SALE-%d", request.IntentID) || !strings.EqualFold(proof.DocumentUUID, request.OperationKey) {
			return false
		}
	} else if proof.Test || !canonicalStockID(proof.DocumentID) || request.NativeWarehouseID <= 0 || request.MovementWarehouseID <= 0 || request.OrganizationID <= 0 || request.RegulationID <= 0 || request.BuyerID <= 0 {
		return false
	}
	if len(request.Event.Lines) == 0 || len(request.Event.Lines) > maxStockProofLines || len(proof.Lines) == 0 || len(proof.Lines) > maxStockProofLines {
		return false
	}
	expected := map[string]*big.Rat{}
	for _, line := range request.Event.Lines {
		if line.VariantID <= 0 || line.Quantity <= 0 || !validProofNomenclature(line.SabyID, request.ExecutionMode) {
			return false
		}
		price, valid := stockProofMoneyValue(line.UnitPrice)
		if !valid {
			return false
		}
		key := line.SabyID + ":" + price.RatString()
		if expected[key] == nil {
			expected[key] = new(big.Rat)
		}
		expected[key].Add(expected[key], new(big.Rat).SetInt64(int64(line.Quantity)))
	}
	var envelope struct {
		Lines []json.RawMessage `json:"lines"`
	}
	if json.Unmarshal(result.StockProof, &envelope) != nil || len(envelope.Lines) != len(proof.Lines) {
		return false
	}
	actual := map[string]*big.Rat{}
	seen := map[int64]bool{}
	for i, line := range proof.Lines {
		var rawLine map[string]json.RawMessage
		if json.Unmarshal(envelope.Lines[i], &rawLine) != nil {
			return false
		}
		for _, name := range []string{"quantity", "unitPrice", "lineSum"} {
			value := bytes.TrimSpace(rawLine[name])
			if len(value) == 0 || value[0] < '0' || value[0] > '9' {
				return false
			}
		}
		if !completeProofObject(envelope.Lines[i], "id", "documentId", "nomenclatureId", "warehouseId", "direction", "quantity", "unitPrice", "lineSum", "closed") || line.ID <= 0 || seen[line.ID] || !line.Closed || line.Direction != 1 ||
			line.DocumentID != proof.DocumentID || line.WarehouseID != request.MovementWarehouseID || !validProofNomenclature(line.NomenclatureID, request.ExecutionMode) || !stockProofQuantity.MatchString(string(line.Quantity)) {
			return false
		}
		quantity, ok := new(big.Rat).SetString(string(line.Quantity))
		if !ok || quantity.Sign() <= 0 {
			return false
		}
		price, valid := stockProofMoneyValue(string(line.UnitPrice))
		sum, validSum := stockProofMoneyValue(string(line.LineSum))
		if !valid || !validSum || new(big.Rat).Mul(quantity, price).Cmp(sum) != 0 {
			return false
		}
		key := line.NomenclatureID + ":" + price.RatString()
		if expected[key] == nil {
			return false
		}
		seen[line.ID] = true
		if actual[key] == nil {
			actual[key] = new(big.Rat)
		}
		actual[key].Add(actual[key], quantity)
	}
	if len(actual) != len(expected) {
		return false
	}
	for id, quantity := range expected {
		if actual[id] == nil || actual[id].Cmp(quantity) != 0 {
			return false
		}
	}
	return true
}

func stockProofMoneyValue(value string) (*big.Rat, bool) {
	if !stockProofMoney.MatchString(value) {
		return nil, false
	}
	n, ok := new(big.Rat).SetString(value)
	return n, ok && n.Sign() >= 0
}

func completeProofObject(raw json.RawMessage, fields ...string) bool {
	var values map[string]json.RawMessage
	if json.Unmarshal(raw, &values) != nil || len(values) != len(fields) {
		return false
	}
	for _, name := range fields {
		if len(values[name]) == 0 || bytes.Equal(bytes.TrimSpace(values[name]), []byte("null")) {
			return false
		}
	}
	return true
}

func canonicalStockID(value string) bool {
	id, err := strconv.ParseInt(value, 10, 64)
	return err == nil && id > 0 && strconv.FormatInt(id, 10) == value
}
func validProofNomenclature(id, mode string) bool {
	return id != "" && strings.TrimSpace(id) == id && (mode == "mock" || canonicalStockID(id))
}

// mockStockProof is deliberately incapable of emitting a live proof. Its row
// IDs and optional zero native IDs are synthetic and always labelled test.
func mockStockProof(request Request, progress Progress) (json.RawMessage, error) {
	if request.ExecutionMode != "mock" {
		return nil, fmt.Errorf("mock stock proof rejects live requests")
	}
	proof := StockProof{Version: 1, Test: true, ExecutionMode: "mock", IntentID: request.IntentID, EventKey: request.Event.Key, EventFingerprint: request.Event.Fingerprint, OperationKey: request.OperationKey,
		DocumentID: progress.DocumentID, DocumentUUID: progress.DocumentUUID, SiteWarehouseID: request.SiteWarehouseID, NativeWarehouseID: request.NativeWarehouseID, MovementWarehouseID: request.MovementWarehouseID,
		OrganizationID: request.OrganizationID, RegulationID: request.RegulationID, BuyerID: request.BuyerID, Posted: true, HasEntries: true, Lines: []StockProofLine{}}
	for i, line := range request.Event.Lines {
		price, ok := stockProofMoneyValue(line.UnitPrice)
		if !ok || line.Quantity <= 0 {
			return nil, fmt.Errorf("mock stock proof rejects missing or invalid money")
		}
		sum := new(big.Rat).Mul(price, new(big.Rat).SetInt64(int64(line.Quantity)))
		proof.Lines = append(proof.Lines, StockProofLine{ID: int64(i + 1), DocumentID: progress.DocumentID, NomenclatureID: line.SabyID, WarehouseID: request.MovementWarehouseID, Direction: 1, Quantity: json.Number(strconv.Itoa(line.Quantity)), UnitPrice: json.Number(line.UnitPrice), LineSum: json.Number(sum.FloatString(2)), Closed: true})
	}
	return json.Marshal(proof)
}
