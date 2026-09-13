-- Only an ephemeral CI database. No customer or supplier production data.
INSERT INTO customers(email,phone,password_hash,full_name,consent_at,retail_discount_bps)
VALUES ('crm-owner@example.invalid','+70000000901','','Владелец проверки',CURRENT_TIMESTAMP,500),
       ('crm-manager@example.invalid','+70000000902','','Менеджер проверки',CURRENT_TIMESTAMP,0);
INSERT INTO admin_users(customer_id,role)
SELECT id,CASE WHEN email='crm-owner@example.invalid' THEN 'owner' ELSE 'manager' END
FROM customers WHERE email IN ('crm-owner@example.invalid','crm-manager@example.invalid');
INSERT INTO auth_sessions(token_hash,customer_id,expires_at)
SELECT encode(sha256(convert_to('crm-acceptance-'||role,'UTF8')),'hex'),customer_id,CURRENT_TIMESTAMP+INTERVAL '1 hour'
FROM admin_users WHERE customer_id IN (SELECT id FROM customers WHERE email IN ('crm-owner@example.invalid','crm-manager@example.invalid'));
INSERT INTO orders(order_number,customer_id,customer_name,phone,email,delivery_method,delivery_fee,subtotal,total,status)
SELECT 'CRM-CHECK-01',id,'Клиент проверки','+70000000901','crm-owner@example.invalid','pickup',0,2490,2490,'new'
FROM customers WHERE email='crm-owner@example.invalid';
INSERT INTO products(name,slug,status,category_id)
SELECT 'Фикус для проверки CRM','crm-acceptance-ficus','published',id FROM categories WHERE slug='plants';
INSERT INTO product_variants(product_id,sku,label,base_price_minor,is_active)
SELECT id,'99999101','D12',249000,0 FROM products WHERE slug='crm-acceptance-ficus';
INSERT INTO saby_nomenclature(saby_id,code,name,price_minor,balance)
VALUES('crm-acceptance-saby','CRM-ACCEPT','Фикус для проверки CRM',249000,4);
UPDATE products SET saby_id='crm-acceptance-saby' WHERE slug='crm-acceptance-ficus';
UPDATE product_variants SET saby_id='crm-acceptance-saby'
WHERE product_id=(SELECT id FROM products WHERE slug='crm-acceptance-ficus');
INSERT INTO product_external_ids(product_id,variant_id,provider,id_type,external_id,status,is_primary,source)
SELECT product.id,variant.id,'ozon','offer_id','crm-acceptance-ozon','active',TRUE,'manual'
FROM products product JOIN product_variants variant ON variant.product_id=product.id
WHERE product.slug='crm-acceptance-ficus';
INSERT INTO product_media(product_id,object_key,sort_order,is_primary)
SELECT id,'/assets/hero-monstera.webp',0,1 FROM products WHERE slug='crm-acceptance-ficus';
INSERT INTO product_media(product_id,object_key,sort_order,is_primary)
SELECT id,'/images/care/light.webp',1,0 FROM products WHERE slug='crm-acceptance-ficus';
INSERT INTO sales_events(channel,source_event_id,source_document_id,source_line_id,cross_source_key,
  event_type,event_status,event_at,external_product_id,saby_id,canonical_variant_id,external_mapping_id,
  units,gross_rub,effect,reconciliation_status,duplicate_of,import_batch_id)
SELECT 'ozon','crm-sale-1','CRM-OZON-001','line-1','crm-shared-1','sale','confirmed',
  CURRENT_TIMESTAMP-INTERVAL '2 days','crm-acceptance-ozon','crm-acceptance-saby',variant.id,mapping.id,
  2,4980,1,'counted',NULL,gen_random_uuid()
FROM product_variants variant JOIN products product ON product.id=variant.product_id
JOIN product_external_ids mapping ON mapping.variant_id=variant.id AND mapping.provider='ozon'
WHERE product.slug='crm-acceptance-ficus';
INSERT INTO sales_events(channel,source_event_id,source_document_id,source_line_id,cross_source_key,
  event_type,event_status,event_at,external_product_id,saby_id,canonical_variant_id,
  units,gross_rub,effect,reconciliation_status,duplicate_of,import_batch_id)
SELECT 'saby','crm-saby-duplicate','CRM-SBIS-001','line-1','crm-shared-1','sale','confirmed',
  CURRENT_TIMESTAMP-INTERVAL '2 days','crm-acceptance-saby','crm-acceptance-saby',variant.id,
  2,4980,1,'duplicate',(SELECT id FROM sales_events WHERE source_event_id='crm-sale-1'),gen_random_uuid()
FROM product_variants variant JOIN products product ON product.id=variant.product_id
WHERE product.slug='crm-acceptance-ficus';
INSERT INTO sales_events(channel,source_event_id,source_document_id,source_line_id,event_type,event_status,
  event_at,external_product_id,saby_id,canonical_variant_id,units,gross_rub,effect,reconciliation_status,import_batch_id)
SELECT 'ozon','crm-return-1','CRM-OZON-RETURN-001','line-1','return','confirmed',CURRENT_TIMESTAMP-INTERVAL '1 day',
  'crm-acceptance-ozon','crm-acceptance-saby',variant.id,1,2490,-1,'counted',gen_random_uuid()
FROM product_variants variant JOIN products product ON product.id=variant.product_id
WHERE product.slug='crm-acceptance-ficus';
INSERT INTO sales_events(channel,source_event_id,source_document_id,source_line_id,event_type,event_status,
  event_at,external_product_id,units,gross_rub,effect,reconciliation_status,import_batch_id)
VALUES('ozon','crm-pending-1','CRM-OZON-PENDING','line-1','sale','pending',CURRENT_TIMESTAMP,
  'unlinked-offer',1,1990,1,'excluded',gen_random_uuid());

-- Stage 05 recommendation, partially allocatable customer demand, and manual
-- supplier follow-up. These rows exist only in the ephemeral CI database.
INSERT INTO saby_nomenclature(saby_id,code,name,price_minor,balance) VALUES
('crm-stage05-demand','CRM-S05-D','Монстера Stage 05',349000,0),
('crm-stage05-wait','CRM-S05-W','Фикус ожидание Stage 05',299000,0),
('crm-stage05-future','CRM-S05-F','Фикус будущая проверка',299000,0);
INSERT INTO products(name,slug,status,category_id,saby_id)
SELECT 'Монстера Stage 05','crm-stage05-demand','draft',id,'crm-stage05-demand' FROM categories WHERE slug='plants';
INSERT INTO product_variants(product_id,sku,label,base_price_minor,is_active,saby_id)
SELECT id,'99999102','D15',349000,0,'crm-stage05-demand' FROM products WHERE slug='crm-stage05-demand';
INSERT INTO procurement_suppliers(name,kind,country_code,default_currency)
VALUES('CRM Stage 05 Supplier','international','NL','EUR');
INSERT INTO procurement_supplier_products(supplier_id,saby_id,supplier_article,availability_status,check_after,unavailable_since,minimum_order_qty,order_multiple,availability_reason,availability_comment,availability_last_action,availability_last_action_at)
SELECT id,'crm-stage05-demand','S05-D','available',NULL,NULL,1,1,'','','marked_available',CURRENT_TIMESTAMP FROM procurement_suppliers WHERE name='CRM Stage 05 Supplier'
UNION ALL SELECT id,'crm-stage05-wait','S05-W','temporarily_unavailable',CURRENT_DATE-1,CURRENT_DATE-7,1,1,'Нет в прайсе','Проверить с менеджером','scheduled_check',CURRENT_TIMESTAMP-INTERVAL '7 days' FROM procurement_suppliers WHERE name='CRM Stage 05 Supplier'
UNION ALL SELECT id,'crm-stage05-future','S05-F','temporarily_unavailable',CURRENT_DATE+7,CURRENT_DATE,1,1,'Ожидаем поставку','Ответ поставщика сохранён','scheduled_check',CURRENT_TIMESTAMP FROM procurement_suppliers WHERE name='CRM Stage 05 Supplier';
INSERT INTO sales_events(channel,source_event_id,source_document_id,source_line_id,event_type,event_status,event_at,external_product_id,saby_id,canonical_variant_id,units,gross_rub,effect,reconciliation_status,import_batch_id)
SELECT 'saby','crm-stage05-demand-sale','CRM-S05-SALE','line-1','sale','confirmed',CURRENT_TIMESTAMP-INTERVAL '8 days','crm-stage05-demand','crm-stage05-demand',id,30,104700,1,'counted',gen_random_uuid()
FROM product_variants WHERE saby_id='crm-stage05-demand';
INSERT INTO procurement_requests(kind,saby_id,requested_name,quantity,customer_order_id,source,notes)
SELECT 'customer_order','crm-stage05-demand','Монстера Stage 05',2,id,'site_order','Проверка частичного распределения'
FROM orders WHERE order_number='CRM-CHECK-01';

-- Stage 06 invoice reconciliation: the plan remains visible beside the
-- current invoice facts. The source PDF is synthetic and contains no PII.
INSERT INTO saby_nomenclature(saby_id,code,name,price_minor,balance) VALUES
('crm-stage06-match','CRM-S06-M','Монстера план Stage 06',53000,0),
('crm-stage06-change','CRM-S06-C','Фикус изменён Stage 06',42000,0),
('crm-stage06-missing','CRM-S06-X','Мухоловка Stage 06',39000,0);
INSERT INTO procurement_suppliers(name,kind,country_code,default_currency)
VALUES('CRM Stage 06 Supplier','international','NL','EUR');
INSERT INTO procurement_orders(supplier_id,order_number,document_number,document_date,source_kind,currency,status,created_by)
SELECT supplier.id,'CRM-STAGE-06','INV-STAGE-06',CURRENT_DATE,'invoice','EUR','review',customer.id
FROM procurement_suppliers supplier,customers customer
WHERE supplier.name='CRM Stage 06 Supplier' AND customer.email='crm-owner@example.invalid';
INSERT INTO procurement_documents(supplier_id,procurement_order_id,file_name,content_type,size_bytes,sha256,content,parser_kind,parser_version,parse_status,arithmetic_status,document_number,document_date,currency,line_count,unit_count,product_subtotal,document_total,calculated_total,extracted_text,created_by,revision_no)
SELECT supplier.id,orders.id,'crm-stage06.pdf','application/pdf',9,repeat('6',64),decode('255044462d5330360a','hex'),'holland_packing_list',2,'review','ok','INV-STAGE-06',CURRENT_DATE,'EUR',3,19,90.80,90.80,90.80,'Synthetic Stage 06 acceptance document',customer.id,1
FROM procurement_suppliers supplier,procurement_orders orders,customers customer
WHERE supplier.name='CRM Stage 06 Supplier' AND orders.order_number='CRM-STAGE-06' AND customer.email='crm-owner@example.invalid';
INSERT INTO procurement_order_lines(procurement_order_id,procurement_document_id,saby_id,raw_name,supplier_article,supplier_category,ordered_qty,invoiced_qty,expected_unit_price,unit_price,line_total,load_unit,pot_diameter_cm,height_cm,match_status,package_count,units_per_package,invoice_raw_name,invoice_supplier_article,reconciliation_status)
SELECT orders.id,documents.id,'crm-stage06-match','Monstera plan','NL-M-12','Monstera',12,12,5.30,5.30,63.60,'1',12,35,'confirmed',1,12,'Monstera invoice','NL-M-12','matched'
FROM procurement_orders orders,procurement_documents documents WHERE orders.order_number='CRM-STAGE-06' AND documents.document_number='INV-STAGE-06'
UNION ALL SELECT orders.id,documents.id,'crm-stage06-change','Ficus plan','NL-F-10','Ficus',6,4,4.20,4.80,19.20,'1',10,30,'confirmed',1,6,'Ficus invoice','NL-F-10','changed' FROM procurement_orders orders,procurement_documents documents WHERE orders.order_number='CRM-STAGE-06' AND documents.document_number='INV-STAGE-06'
UNION ALL SELECT orders.id,NULL,'crm-stage06-missing','Venus flytrap','NL-V-09','Carnivorous',8,NULL,3.90,NULL,NULL,'1',9,15,'confirmed',1,8,'','','missing' FROM procurement_orders orders WHERE orders.order_number='CRM-STAGE-06'
UNION ALL SELECT orders.id,documents.id,NULL,'Calathea supplier addition','NL-NEW-1','Calathea',0,3,NULL,2.67,8.00,'1',12,30,'new_product',NULL,NULL,'Calathea supplier addition','NL-NEW-1','added' FROM procurement_orders orders,procurement_documents documents WHERE orders.order_number='CRM-STAGE-06' AND documents.document_number='INV-STAGE-06';

-- Stage 07 calculation and external-action states. The receipt is a synthetic
-- draft waiting for manual posting; no external call is made in CI.
INSERT INTO saby_nomenclature(saby_id,code,name,price_minor,balance) VALUES
('crm-stage07-a','CRM-S07-A','Антуриум Stage 07',189000,5),
('crm-stage07-b','CRM-S07-B','Фикус Stage 07',179000,7);
INSERT INTO products(name,slug,status,category_id,saby_id)
SELECT 'Антуриум Stage 07','crm-stage07-a','draft',id,'crm-stage07-a' FROM categories WHERE slug='plants'
UNION ALL SELECT 'Фикус Stage 07','crm-stage07-b','draft',id,'crm-stage07-b' FROM categories WHERE slug='plants';
INSERT INTO product_variants(product_id,sku,label,base_price_minor,is_active,saby_id,current_unit_cost_rub,current_unit_cost_kind,current_unit_cost_effective_at)
SELECT id,'99999107','D17',189000,0,'crm-stage07-a',945,'estimated',CURRENT_TIMESTAMP FROM products WHERE slug='crm-stage07-a'
UNION ALL SELECT id,'99999108','D12',179000,0,'crm-stage07-b',895,'estimated',CURRENT_TIMESTAMP FROM products WHERE slug='crm-stage07-b';
INSERT INTO procurement_cost_history(canonical_variant_id,saby_id,unit_cost_rub,cost_kind,source,effective_at)
SELECT id,saby_id,current_unit_cost_rub,'estimated','saby_retail_half_initial',CURRENT_TIMESTAMP FROM product_variants WHERE saby_id IN ('crm-stage07-a','crm-stage07-b');
INSERT INTO procurement_suppliers(name,kind,country_code,default_currency)
VALUES('CRM Stage 07 Supplier','international','NL','EUR');
INSERT INTO procurement_orders(supplier_id,order_number,document_number,document_date,source_kind,currency,status,
  exchange_rate,delivery_to_moscow_rub,delivery_to_ryazan_rub,trolley_cost_rub,calculation_version,calculation_settings,calculated_at,created_by)
SELECT supplier.id,'CRM-STAGE-07','INV-STAGE-07',CURRENT_DATE,'invoice','EUR','ready_to_receive',115,12000,1700,12000,1,'{}',CURRENT_TIMESTAMP,customer.id
FROM procurement_suppliers supplier,customers customer WHERE supplier.name='CRM Stage 07 Supplier' AND customer.email='crm-owner@example.invalid';
INSERT INTO procurement_documents(supplier_id,procurement_order_id,file_name,content_type,size_bytes,sha256,content,parser_kind,parser_version,parse_status,arithmetic_status,document_number,document_date,currency,line_count,unit_count,product_subtotal,document_total,calculated_total,extracted_text,created_by,revision_no)
SELECT supplier.id,orders.id,'crm-stage07.pdf','application/pdf',9,repeat('7',64),decode('255044462d5330370a','hex'),'holland_packing_list',2,'parsed','ok','INV-STAGE-07',CURRENT_DATE,'EUR',2,18,91.80,91.80,91.80,'Synthetic Stage 07 acceptance document',customer.id,1
FROM procurement_suppliers supplier,procurement_orders orders,customers customer WHERE supplier.name='CRM Stage 07 Supplier' AND orders.order_number='CRM-STAGE-07' AND customer.email='crm-owner@example.invalid';
INSERT INTO procurement_order_lines(procurement_order_id,procurement_document_id,saby_id,canonical_variant_id,raw_name,supplier_article,supplier_category,ordered_qty,invoiced_qty,expected_unit_price,unit_price,line_total,load_unit,pot_diameter_cm,height_cm,match_status,package_count,units_per_package,invoice_raw_name,invoice_supplier_article,reconciliation_status,purchase_unit_rub,trolley_delivery_unit_rub,ryazan_delivery_unit_rub,unit_cost_rub,proposed_retail_rub,proposed_marketplace_rub,proposed_marketplace_strike_rub)
SELECT orders.id,documents.id,'crm-stage07-a',variant.id,'Anthurium','S07-A','Anthurium',6,6,5.30,5.30,31.80,'1',17,60,'confirmed',1,6,'Anthurium','S07-A','matched',609.5,600,100,1309.5,2790,4890,6357
FROM procurement_orders orders,procurement_documents documents,product_variants variant WHERE orders.order_number='CRM-STAGE-07' AND documents.document_number='INV-STAGE-07' AND variant.saby_id='crm-stage07-a'
UNION ALL SELECT orders.id,documents.id,'crm-stage07-b',variant.id,'Ficus','S07-B','Ficus',12,12,5.00,5.00,60.00,'1',12,40,'confirmed',2,6,'Ficus','S07-B','matched',575,700,91.666667,1366.666667,2790,4690,6097
FROM procurement_orders orders,procurement_documents documents,product_variants variant WHERE orders.order_number='CRM-STAGE-07' AND documents.document_number='INV-STAGE-07' AND variant.saby_id='crm-stage07-b';
INSERT INTO procurement_action_batches(procurement_order_id,kind,status,created_by,calculation_version,calculated_at)
SELECT id,'receipt','processing',created_by,calculation_version,calculated_at FROM procurement_orders WHERE order_number='CRM-STAGE-07';
INSERT INTO procurement_action_items(batch_id,procurement_order_line_id,channel,external_article,new_value,quantity,status,external_operation_id,external_url,locked_until,payload)
SELECT batch.id,MIN(line.id),'saby_receipt',orders.id::TEXT,0,SUM(line.invoiced_qty),'processing','7707','https://ret.saby.ru/opendoc.html?guid=crm-stage-07',CURRENT_TIMESTAMP+INTERVAL '1 day',
  jsonb_build_object('lines',jsonb_agg(jsonb_build_object('sabyId',line.saby_id,'code',n.code,'name',n.name,'quantity',line.invoiced_qty,'unitCost',line.unit_cost_rub,'oldBalance',n.balance,'newBalance',n.balance+line.invoiced_qty) ORDER BY line.id))
FROM procurement_action_batches batch JOIN procurement_orders orders ON orders.id=batch.procurement_order_id
JOIN procurement_order_lines line ON line.procurement_order_id=orders.id JOIN saby_nomenclature n ON n.saby_id=line.saby_id
WHERE orders.order_number='CRM-STAGE-07' AND batch.kind='receipt' GROUP BY batch.id,orders.id;
