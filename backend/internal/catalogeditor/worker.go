// Saby preview simulates explicit mock publications in isolated local/staging
// databases. Disabled snapshots remain inert; the real Saby API is not called.
package catalogeditor

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"log/slog"
	"strings"
	"time"
)

func RunPreview(ctx context.Context, pool *pgxpool.Pool, environment string, logger *slog.Logger) {
	if environment != "development" && environment != "staging" {
		return
	}
	var database string
	if err := pool.QueryRow(ctx, "SELECT current_database()").Scan(&database); err != nil {
		return
	}
	if !strings.HasPrefix(database, "ficusin_local_") && !strings.HasPrefix(database, "ficusin_staging") {
		logger.Error("editor preview refused a non-isolated database")
		return
	}
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		if err := step(ctx, pool); err != nil && ctx.Err() == nil {
			logger.Error("catalog editor preview failed", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
func step(ctx context.Context, pool *pgxpool.Pool) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var id int64
	var channel string
	var raw []byte
	err = tx.QueryRow(ctx, `SELECT variant_id,channel FROM catalog_editor_publications WHERE status='prepared' AND execution_mode='mock' AND channel='saby' ORDER BY updated_at LIMIT 1`).Scan(&id, &channel)
	if err == pgx.ErrNoRows {
		return nil
	}
	if err != nil {
		return err
	}
	var locked int64
	err = tx.QueryRow(ctx, `SELECT id FROM product_variants WHERE id=$1 FOR UPDATE SKIP LOCKED`, id).Scan(&locked)
	if err == pgx.ErrNoRows {
		return nil
	}
	if err != nil {
		return err
	}
	err = tx.QueryRow(ctx, `SELECT payload FROM catalog_editor_publications WHERE variant_id=$1 AND channel=$2 AND status='prepared' AND execution_mode='mock' FOR UPDATE`, id, channel).Scan(&raw)
	if err == pgx.ErrNoRows {
		return nil
	}
	if err != nil {
		return err
	}
	var p struct {
		Name        string   `json:"name"`
		Description string   `json:"description"`
		SKU         string   `json:"sku"`
		Barcode     string   `json:"barcode"`
		SabyID      string   `json:"sabyId"`
		PriceMinor  int64    `json:"priceMinor"`
		Images      []string `json:"images"`
		Saby        struct {
			Folder struct {
				ID string `json:"id"`
			} `json:"folder"`
		} `json:"saby"`
	}
	if err = json.Unmarshal(raw, &p); err != nil {
		return err
	}
	message := "Тестовая подготовка завершена. Внешняя площадка не изменялась."
	if channel == "saby" && p.SabyID == "" {
		remote := "STG-EDITOR-" + p.Barcode
		_, err = tx.Exec(ctx, `INSERT INTO saby_nomenclature(saby_id,code,article,barcode,name,description,price_minor,balance,images,folder_saby_id) VALUES($1,$2,$3,$2,$4,$5,$6,0,$7,NULLIF($8,'')) ON CONFLICT(saby_id) DO NOTHING`, remote, p.Barcode, p.SKU, p.Name, p.Description, p.PriceMinor, p.Images, p.Saby.Folder.ID)
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `UPDATE product_variants SET saby_id=$2 WHERE id=$1 AND COALESCE(saby_id,'')=''`, id, remote)
		if err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `INSERT INTO product_external_ids(product_id,variant_id,provider,id_type,external_id,source) SELECT product_id,id,'saby','id',$2,'local-preview' FROM product_variants WHERE id=$1 ON CONFLICT(provider,id_type,external_id) DO NOTHING`, id, remote); err != nil {
			return err
		}
		message = fmt.Sprintf("Тестовый товар СБИС %s создан. Рабочий СБИС не изменялся.", remote)
	}
	_, err = tx.Exec(ctx, `UPDATE catalog_editor_publications SET status='simulated',message=$3,updated_at=NOW() WHERE variant_id=$1 AND channel=$2`, id, channel, message)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}
