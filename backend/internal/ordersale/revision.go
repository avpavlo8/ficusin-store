package ordersale

import (
	"context"
	"encoding/json"
	"time"
)

// RevisionCommand keeps immutable document ownership separate from both content
// plans. Updating an order can never manufacture a second Saby document.
type RevisionCommand struct {
	Origin            Request
	Current           Request
	Target            Request
	RevisionID        int64
	CurrentRevisionID int64
}
type RevisionProvider interface {
	StepRevision(context.Context, RevisionCommand, Progress) (Result, error)
}
type RevisionView struct {
	ID        int64      `json:"id"`
	Number    int64      `json:"number"`
	State     string     `json:"state"`
	CreatedAt time.Time  `json:"createdAt"`
	AppliedAt *time.Time `json:"appliedAt,omitempty"`
}

// RevisionRetirementProof is a bound fresh readback that specific previously
// owned rows are absent. It is not inferred from a transport acknowledgement.
type RevisionRetirementProof struct {
	Version         int             `json:"version"`
	RevisionID      int64           `json:"revisionId"`
	OriginHash      string          `json:"originHash"`
	CurrentHash     string          `json:"currentHash"`
	TargetHash      string          `json:"targetHash"`
	DocumentID      string          `json:"documentId"`
	DocumentUUID    string          `json:"documentUUID"`
	RetiredRowIDs   []int64         `json:"retiredRowIds"`
	RemainingRowIDs []int64         `json:"remainingRowIds"`
	Readback        json.RawMessage `json:"readback"`
}

func revisionWriteStage(stage string) bool {
	switch stage {
	case "revision-bind", "revision-unpost", "revision-delete", "revision-prepare", "revision-save", "revision-post", "revision-cancel":
		return true
	}
	return false
}
