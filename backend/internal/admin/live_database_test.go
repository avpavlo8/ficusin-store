package admin

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/avpavlo8/ficusin-store/backend/internal/catalog"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Это единственное место, где бизнес-логика каталога и заказа исполняется на
// настоящей PostgreSQL со всеми применёнными миграциями.
//
// Остальные проверки этого не делают: контрактные тесты сверяют подстроки в
// исходниках, Playwright работает против замоканного API, а гейт миграций
// применяет SQL без единого запроса приложения. Именно в эту щель ушли в прод
// невалидный SQL витрины и редактирование заказа, оставшееся на схеме до
// миграции 056.
func TestCatalogueAndOrderEditingOnLiveDatabase(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL не задан: живая проверка пропущена")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatalf("подключение к базе: %v", err)
	}
	defer pool.Close()

	unique := time.Now().UnixNano() % 1000000000
	smallSKU := fmt.Sprint(500000000 + unique)
	largeSKU := fmt.Sprint(600000000 + unique)
	productName := fmt.Sprintf("Интеграционная аглаонема %d", unique)

	var productID, smallVariant, largeVariant, warehouseID, orderID, staffID int64
	// Аудит админки ссылается на карточку сотрудника внешним ключом, поэтому
	// актёр обязан существовать.
	if err := pool.QueryRow(ctx,
		`INSERT INTO customers(email,phone,password_hash,full_name,consent_at) VALUES($1,$2,'','Интеграционный владелец',CURRENT_TIMESTAMP) RETURNING id`,
		fmt.Sprintf("ci-%d@example.invalid", unique), fmt.Sprintf("+7900%07d", unique%10000000),
	).Scan(&staffID); err != nil {
		t.Fatalf("завести сотрудника: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`INSERT INTO products(name,slug,status) VALUES($1,$2,'published') RETURNING id`,
		productName, fmt.Sprintf("integration-%d", unique),
	).Scan(&productID); err != nil {
		t.Fatalf("завести товар: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`INSERT INTO product_variants(product_id,sku,label,base_price_minor,is_active) VALUES($1,$2,'D12',149000,1) RETURNING id`,
		productID, smallSKU,
	).Scan(&smallVariant); err != nil {
		t.Fatalf("завести малый SKU: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`INSERT INTO product_variants(product_id,sku,label,base_price_minor,is_active) VALUES($1,$2,'D25',249000,1) RETURNING id`,
		productID, largeSKU,
	).Scan(&largeVariant); err != nil {
		t.Fatalf("завести большой SKU: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`INSERT INTO warehouses(name,city,address) VALUES('CI','Рязань','') RETURNING id`,
	).Scan(&warehouseID); err != nil {
		t.Fatalf("завести склад: %v", err)
	}
	for _, variant := range []int64{smallVariant, largeVariant} {
		if _, err := pool.Exec(ctx,
			`INSERT INTO inventory(warehouse_id,variant_id,available_qty) VALUES($1,$2,10)`,
			warehouseID, variant,
		); err != nil {
			t.Fatalf("завести остаток: %v", err)
		}
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO orders(order_number,customer_name,phone,email,delivery_method,delivery_fee,subtotal,total,status)
		VALUES($1,'Интеграционный тест','+70000000000','ci@example.invalid','pickup',0,1490,1490,'new')
		RETURNING id
	`, fmt.Sprintf("CI-%d", unique)).Scan(&orderID); err != nil {
		t.Fatalf("завести заказ: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO order_items(order_id,product_id,variant_id,sku,product_name,variant_label,unit_price,quantity)
		VALUES($1,$2,$3,$4,$5,'D12',1490,1)
	`, orderID, productID, smallVariant, smallSKU, productName); err != nil {
		t.Fatalf("завести строку заказа: %v", err)
	}

	t.Run("витрина исполняется и отдаёт SKU", func(t *testing.T) {
		products, err := catalog.NewPostgresRepository(pool).ListAvailable(ctx)
		if err != nil {
			t.Fatalf("запрос каталога не выполняется на мигрированной схеме: %v", err)
		}
		var found *catalog.Product
		for index := range products {
			if products[index].Name == productName {
				found = &products[index]
				break
			}
		}
		if found == nil {
			t.Fatalf("засеянный товар не попал в каталог (всего товаров: %d)", len(products))
		}
		if found.SKU == "" {
			t.Error("карточка каталога пришла без SKU")
		}
		if found.Price <= 0 {
			t.Errorf("цена карточки: %v", found.Price)
		}
	})

	t.Run("черновик без фотографии можно опубликовать", func(t *testing.T) {
		var categoryID, draftID int64
		if err := pool.QueryRow(ctx, `INSERT INTO categories(name,slug) VALUES($1,$2) RETURNING id`,
			"Категория без обязательного фото", fmt.Sprintf("no-photo-%d", unique)).Scan(&categoryID); err != nil {
			t.Fatalf("завести категорию: %v", err)
		}
		if err := pool.QueryRow(ctx, `INSERT INTO products(category_id,name,slug,status) VALUES($1,$2,$3,'draft') RETURNING id`,
			categoryID, "Товар без фотографии", fmt.Sprintf("no-photo-product-%d", unique)).Scan(&draftID); err != nil {
			t.Fatalf("завести черновик: %v", err)
		}
		if _, err := pool.Exec(ctx, `INSERT INTO product_variants(product_id,sku,label,base_price_minor,is_active) VALUES($1,$2,'Основной',10000,1)`,
			draftID, fmt.Sprintf("NO-PHOTO-%d", unique)); err != nil {
			t.Fatalf("завести вариант: %v", err)
		}
		result, err := NewPostgresRepository(pool).PublishDraftProducts(ctx, Actor{CustomerID: staffID, Role: RoleOwner}, []int64{draftID})
		if err != nil {
			t.Fatalf("опубликовать без фотографии: %v", err)
		}
		if len(result.Published) != 1 || result.Published[0] != draftID || len(result.Blocked) != 0 {
			t.Fatalf("неожиданный результат публикации: %+v", result)
		}
	})

	t.Run("редактирование заказа пишет полный кортеж", func(t *testing.T) {
		items := []OrderEditLine{{SKU: largeSKU, Quantity: 2}}
		if _, err := NewPostgresRepository(pool).EditOrder(
			ctx, Actor{CustomerID: staffID, Role: RoleOwner}, orderID, OrderEdit{Items: &items},
		); err != nil {
			t.Fatalf("редактирование состава заказа: %v", err)
		}

		var storedProduct, storedVariant int64
		var storedSKU, storedLabel string
		var quantity int
		var price float64
		if err := pool.QueryRow(ctx, `
			SELECT product_id,variant_id,sku,variant_label,quantity,unit_price::DOUBLE PRECISION
			FROM order_items WHERE order_id=$1
		`, orderID).Scan(&storedProduct, &storedVariant, &storedSKU, &storedLabel, &quantity, &price); err != nil {
			t.Fatalf("прочитать отредактированный заказ: %v", err)
		}
		if storedProduct != productID {
			t.Errorf("product_id: %d, ожидали %d — строка заказа ссылается на чужой товар", storedProduct, productID)
		}
		if storedVariant != largeVariant {
			t.Errorf("variant_id: %d, ожидали %d — в заказ попал не тот размер", storedVariant, largeVariant)
		}
		if storedSKU != largeSKU {
			t.Errorf("sku: %q, ожидали %q", storedSKU, largeSKU)
		}
		if storedLabel != "D25" {
			t.Errorf("variant_label: %q, ожидали D25", storedLabel)
		}
		if quantity != 2 {
			t.Errorf("количество: %d, ожидали 2", quantity)
		}
		if price != 2490 {
			t.Errorf("цена: %v, ожидали 2490 — новая позиция считается по цене витрины", price)
		}
	})
}
