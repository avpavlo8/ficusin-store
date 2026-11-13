package ordersale

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

type Request struct {
	BuyerID             int64  `json:"buyerId,omitempty"`
	IntentID            int64  `json:"intentId"`
	Event               Event  `json:"event"`
	SiteWarehouseID     int64  `json:"siteWarehouseId"`
	NativeWarehouseID   int64  `json:"nativeWarehouseId"`
	MovementWarehouseID int64  `json:"movementWarehouseId"`
	OrganizationID      int64  `json:"organizationId"`
	RegulationID        int64  `json:"regulationId"`
	OperationKey        string `json:"operationKey"`
	ExecutionMode       string `json:"executionMode"`
}

// Native is a durable schema-labelled receipt. RawMessage avoids float64 loss
// when PostgreSQL JSONB normalizes a prepared native record between steps.
type Progress struct {
	StockBlocked          bool            `json:"stockBlocked,omitempty"`
	DesiredState          string          `json:"desiredState,omitempty"`
	CreateAttempted       bool            `json:"createAttempted,omitempty"`
	PostAttempted         bool            `json:"postAttempted,omitempty"`
	OwnedRowIDs           []int64         `json:"ownedRowIds,omitempty"`
	ConflictingStockProof json.RawMessage `json:"conflictingStockProof,omitempty"`
	IdentityConflict      bool            `json:"identityConflict,omitempty"`
	Stage                 string          `json:"stage"`
	DocumentID            string          `json:"documentId,omitempty"`
	DocumentUUID          string          `json:"documentUUID,omitempty"`
	Native                json.RawMessage `json:"native,omitempty"`
	Unknown               bool            `json:"unknown,omitempty"`
}
type Result struct {
	CancellationVerified bool
	CancellationProof    json.RawMessage
	Progress             Progress
	State                string
	Message              string
	StockVerified        bool
	StockProof           json.RawMessage
	RetryAfter           time.Duration
}

// Only create/fill/post/cancel may write. Every other stage, including reconcile-*,
// must be read-only. A returned next write stage is committed before invocation.
type Provider interface {
	Step(context.Context, Request, Progress) (Result, error)
}
type DispatchControl struct {
	BuyerID             int64
	Mode                string
	Enabled             bool
	SiteWarehouseID     int64
	NativeWarehouseID   int64
	MovementWarehouseID int64
	OrganizationID      int64
	RegulationID        int64
}
type PublicationView struct {
	CancellationKind       string     `json:"cancellationKind,omitempty"`
	DesiredState           string     `json:"desiredState"`
	CancellationVerified   bool       `json:"cancellationVerified"`
	StockVerifiedAt        *time.Time `json:"stockVerifiedAt,omitempty"`
	CancellationVerifiedAt *time.Time `json:"cancellationVerifiedAt,omitempty"`
	StockVerified          bool       `json:"stockVerified"`
	TestMode               bool       `json:"testMode"`
	SiteWarehouseID        int64      `json:"siteWarehouseId"`
	ID                     int64      `json:"id"`
	ExecutionMode          string     `json:"executionMode"`
	Status                 string     `json:"status"`
	Message                string     `json:"message"`
	DocumentID             string     `json:"documentId"`
	DocumentUUID           string     `json:"documentUUID"`
	Attempts               int        `json:"attempts"`
	CanRetry               bool       `json:"canRetry"`
	UpdatedAt              time.Time  `json:"updatedAt"`
}

var ErrRetryUnsafe = errors.New("неизвестный результат создания требует ручной сверки; новый документ не создавался")

func newOperationKey() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	b[6] = (b[6] & 15) | 64
	b[8] = (b[8] & 63) | 128
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[:4], b[4:6], b[6:8], b[8:10], b[10:]), nil
}
func requestHash(request Request) (string, error) {
	raw, err := json.Marshal(request)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}
func writeStage(stage string) bool {
	return stage == "create" || stage == "fill" || stage == "post" || stage == "cancel"
}
func recoverProgress(progress Progress) (Progress, bool) {
	if !writeStage(progress.Stage) {
		return progress, true
	}
	progress.Unknown = true
	if progress.DocumentID == "" {
		return progress, false
	}
	progress.Stage = "reconcile-" + progress.Stage
	return progress, true
}
func retrySafe(status string, progress Progress) bool {
	if progress.IdentityConflict {
		return false
	}
	if status == "needs_attention" && progress.StockBlocked && !progress.PostAttempted {
		return true
	}
	if progress.DesiredState == "cancelled" && (status == "failed" || status == "needs_attention") {
		return true
	}
	if progress.Unknown && progress.DocumentID == "" {
		return false
	}
	if status == "failed" {
		return !writeStage(progress.Stage) || (progress.DocumentID != "")
	}
	return status == "needs_attention" && progress.Unknown && progress.DocumentID != ""
}
