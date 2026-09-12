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
INSERT INTO product_media(product_id,object_key,sort_order,is_primary)
SELECT id,'/assets/hero-monstera.webp',0,1 FROM products WHERE slug='crm-acceptance-ficus';
INSERT INTO product_media(product_id,object_key,sort_order,is_primary)
SELECT id,'/images/care/light.webp',1,0 FROM products WHERE slug='crm-acceptance-ficus';
