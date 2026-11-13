// Package ordersale coordinates durable handover intents and publication steps.
// External writes require an explicitly configured provider; staging stays mock.
package ordersale

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

var ErrInput = errors.New("проверьте выбранный склад и отправки")
var ErrConflict = errors.New("состав отправки изменился; обновите список")
var ErrNotReady = errors.New("передача заказа ещё не подтверждена")

type Line struct {
	OrderItemID int64  `json:"orderItemId"`
	VariantID   int64  `json:"variantId"`
	Name        string `json:"name"`
	SKU         string `json:"sku"`
	SabyID      string `json:"sabyId"`
	Quantity    int    `json:"quantity"`
	UnitPrice   string `json:"unitPrice"`
}
type Event struct {
	Publication        *PublicationView `json:"publication,omitempty"`
	Key                string           `json:"key"`
	OrderID            int64            `json:"orderId"`
	OrderNumber        string           `json:"orderNumber"`
	ShipmentOfferID    int64            `json:"shipmentOfferId"`
	DeliveryMethod     string           `json:"deliveryMethod"`
	Status             string           `json:"status"`
	CDEKStatus         string           `json:"cdekStatus"`
	Lines              []Line           `json:"lines"`
	IntentID           int64            `json:"intentId"`
	State              string           `json:"state"`
	Source             string           `json:"source"`
	WarehouseID        int64            `json:"warehouseId"`
	PreparedAt         *time.Time       `json:"preparedAt"`
	CreatedAt          time.Time        `json:"createdAt"`
	MappingConflict    bool             `json:"mappingConflict"`
	QuantityConflict   bool             `json:"quantityConflict"`
	Fingerprint        string           `json:"fingerprint"`
	Blockers           []string         `json:"blockers"`
	CanPrepare         bool             `json:"canPrepare"`
	ExternalDocumentID string           `json:"externalDocumentId"`
}
type Warehouse struct {
	ID                     int64  `json:"id"`
	Name                   string `json:"name"`
	NativeMappingConfirmed bool   `json:"nativeMappingConfirmed"`
}
type View struct {
	ExecutionMode         string      `json:"executionMode"`
	DispatchEnabled       bool        `json:"dispatchEnabled"`
	DispatchMode          string      `json:"dispatchMode"`
	AutoDispatchEnabled   bool        `json:"autoDispatchEnabled"`
	Events                []Event     `json:"events"`
	Warehouses            []Warehouse `json:"warehouses"`
	HasMore               bool        `json:"hasMore"`
	ExternalWritesEnabled bool        `json:"externalWritesEnabled"`
}
type Selection struct {
	OrderID         int64  `json:"orderId"`
	ShipmentOfferID int64  `json:"shipmentOfferId"`
	Fingerprint     string `json:"fingerprint"`
}
type PrepareInput struct {
	WarehouseID int64       `json:"warehouseId"`
	Events      []Selection `json:"events"`
}

// The same physical milestones already recognized by order.cdekStatusToOrder.
// CREATED/ACCEPTED alone do not prove handover and cannot enqueue a realization.
func HandedOver(delivery, status, carrierStatus string) bool {
	if delivery == "pickup" {
		return status == "completed"
	}
	if delivery != "cdek" || (status != "shipped" && status != "completed") {
		return false
	}
	switch strings.ToUpper(carrierStatus) {
	case "RECEIVED_AT_SHIPMENT_WAREHOUSE", "READY_FOR_SHIPMENT_IN_SENDER_CITY", "TAKEN_BY_TRANSPORTER_FROM_SENDER_CITY", "SENT_TO_RECIPIENT_CITY", "IN_TRANSIT", "ACCEPTED_IN_TRANSIT_CITY", "ACCEPTED_AT_TRANSIT_WAREHOUSE", "RECEIVED_AT_DELIVERY_WAREHOUSE", "ACCEPTED_AT_PICK_UP_POINT", "POSTOMAT_POSTED", "READY_FOR_RECIPIENT", "DELIVERED":
		return true
	}
	return false
}
func eventKey(orderID, offerID int64) string {
	if offerID > 0 {
		return fmt.Sprintf("shipment:%d", offerID)
	}
	return fmt.Sprintf("order:%d", orderID)
}
func finish(event *Event) {
	event.Blockers = []string{}
	event.CanPrepare = HandedOver(event.DeliveryMethod, event.Status, event.CDEKStatus) && len(event.Lines) > 0
	if !HandedOver(event.DeliveryMethod, event.Status, event.CDEKStatus) {
		event.Blockers = append(event.Blockers, "Передача покупателю или СДЭК не подтверждена")
	}
	if len(event.Lines) == 0 {
		event.Blockers = append(event.Blockers, "Нет новых переданных позиций для реализации")
	}
	for _, line := range event.Lines {
		if line.VariantID == 0 || strings.TrimSpace(line.SabyID) == "" {
			event.CanPrepare = false
			event.Blockers = append(event.Blockers, "Не указан товар СБИС: "+line.Name)
		}
	}
	if event.MappingConflict {
		event.CanPrepare = false
		event.Blockers = append(event.Blockers, "Связь варианта со СБИС изменилась после передачи; нужна сверка")
	}
	if event.QuantityConflict {
		event.CanPrepare = false
		event.Blockers = append(event.Blockers, "Количество пересекается с другой реализацией; нужна сверка отправок")
	}

	// Exclude mutable status/timestamps: shipped -> completed is the same event.
	payload, _ := json.Marshal(struct {
		Key   string
		Lines []Line
	}{event.Key, event.Lines})
	sum := sha256.Sum256(payload)
	event.Fingerprint = hex.EncodeToString(sum[:])
}
