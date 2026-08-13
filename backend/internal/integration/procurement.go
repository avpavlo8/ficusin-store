package integration

import (
	"context"
	"fmt"

	"github.com/avpavlo8/ficusin-store/backend/internal/procurement"
)

// ProcurementExecutor keeps failures isolated by channel. A broken Saby
// session must not prevent WB or Ozon actions from leaving the durable queue.
type ProcurementExecutor struct {
	marketplaces *MarketplaceExecutor
	saby         *SabyClient
}

func NewProcurementExecutor(marketplaces *MarketplaceExecutor, saby *SabyClient) *ProcurementExecutor {
	return &ProcurementExecutor{marketplaces: marketplaces, saby: saby}
}

func (executor *ProcurementExecutor) Configured(channel string) bool {
	switch channel {
	case "wb", "ozon":
		return executor != nil && executor.marketplaces != nil && executor.marketplaces.Configured(channel)
	case "saby":
		return executor != nil && executor.saby != nil && executor.saby.Configured()
	// Public Saby Retail documentation exposes price lists for reading, but
	// does not document a write endpoint. Keep write channels disabled until
	// a supported contract is configured; never report a guessed write as OK.
	case "saby_price", "saby_receipt":
		return false
	default:
		return false
	}
}

func (executor *ProcurementExecutor) Execute(ctx context.Context, item procurement.ActionItem) (procurement.ActionExecution, error) {
	switch item.Channel {
	case "wb", "ozon":
		return executor.marketplaces.Execute(ctx, item)
	default:
		return procurement.ActionExecution{}, fmt.Errorf("канал %s не поддерживается", item.Channel)
	}
}

func (executor *ProcurementExecutor) Probe(ctx context.Context, channel string) error {
	switch channel {
	case "wb", "ozon":
		return executor.marketplaces.Probe(ctx, channel)
	case "saby":
		return executor.saby.Probe(ctx)
	default:
		return fmt.Errorf("канал %s не поддерживается", channel)
	}
}
