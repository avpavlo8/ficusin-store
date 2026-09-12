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
