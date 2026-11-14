-- A real outgoing document must be bound to an explicitly verified buyer.
-- Do not rewrite already frozen publication requests or guess a counterparty.
ALTER TABLE order_sale_dispatch_control ADD COLUMN buyer_id BIGINT NOT NULL DEFAULT 0;
-- An earlier local build may already have activated control without this
-- binding. Stop new dispatch until validated application configuration refreshes it.
UPDATE order_sale_dispatch_control SET enabled=FALSE WHERE execution_mode='live' AND buyer_id<=0;
ALTER TABLE order_sale_dispatch_control ADD CONSTRAINT order_sale_dispatch_control_live_buyer
 CHECK(NOT enabled OR execution_mode<>'live' OR buyer_id>0);
