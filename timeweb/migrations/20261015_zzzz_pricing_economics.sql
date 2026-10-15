-- Defaults record the owner's monthly expense estimate. Unknown seller tariffs
-- remain absent: zero charges must be entered explicitly after checking them.
ALTER TABLE procurement_pricing_settings ADD COLUMN economics JSONB NOT NULL
 DEFAULT '{"monthlyFixedCostsRub":300000,"channels":{}}'::JSONB;
ALTER TABLE procurement_order_lines ADD COLUMN channel_costs JSONB NOT NULL DEFAULT '{}'::JSONB,
 ADD COLUMN channel_prices JSONB NOT NULL DEFAULT '{}'::JSONB,
 ADD COLUMN channel_quotes JSONB NOT NULL DEFAULT '{}'::JSONB,
 ADD COLUMN retail_override_rub BIGINT CHECK(retail_override_rub>0),
 ADD COLUMN dimensions_reviewed BOOLEAN NOT NULL DEFAULT FALSE;
-- Previously calculated marketplace amounts used a shared additive surcharge.
-- Do not rewrite historical actions, costs, prices or completed procurements.
-- Require a fresh calculation before new actions can use the new formula.
UPDATE procurement_orders SET calculation_version=NULL,calculated_at=NULL,
 calculation_settings=NULL,status=CASE WHEN status='ready_to_receive' THEN 'review' ELSE status END
 WHERE status NOT IN('received','cancelled')
 AND NOT EXISTS(SELECT 1 FROM procurement_action_batches b WHERE b.procurement_order_id=procurement_orders.id
 AND b.status NOT IN('draft','cancelled','completed'))
 AND NOT EXISTS(SELECT 1 FROM procurement_action_batches b JOIN procurement_action_items i ON i.batch_id=b.id
 WHERE b.procurement_order_id=procurement_orders.id AND i.channel='saby_receipt' AND i.status='completed');
UPDATE procurement_action_batches b SET status='cancelled',updated_at=CURRENT_TIMESTAMP
 WHERE b.status='draft' AND EXISTS(SELECT 1 FROM procurement_orders o
 WHERE o.id=b.procurement_order_id AND o.calculated_at IS NULL);

ALTER TABLE procurement_orders ADD COLUMN supplier_charges_rub NUMERIC(18,2) NOT NULL DEFAULT 0,
 ADD COLUMN returnable_deposit_rub NUMERIC(18,2) NOT NULL DEFAULT 0,
 ADD COLUMN additional_costs_rub NUMERIC(18,2) NOT NULL DEFAULT 0 CHECK(additional_costs_rub>=0);
ALTER TABLE procurement_order_lines ADD COLUMN additional_purchase_unit_rub NUMERIC(18,6);
