package admin

import (
	"context"

	"github.com/jackc/pgx/v5"
)

// A creation checkbox never disables an established channel. Live and mock
// receipts stay separate; canonical catalogue links also identify existing cards.
func editorLinkedChannels(ctx context.Context, tx pgx.Tx, variantID int64, mode string) (map[string]string, error) {
	links := map[string]string{}
	// Saby metadata belongs to EditorPlanView.SabyLinks, not marketplace cards.
	rows, e := tx.Query(ctx, `SELECT channel,CASE WHEN channel='wb' THEN remote_id ELSE offer_id END FROM catalog_editor_remote_links WHERE variant_id=$1 AND execution_mode=$2 AND channel IN ('wb','ozon')`, variantID, mode)
	if e != nil {
		return nil, e
	}
	for rows.Next() {
		var channel, id string
		if e = rows.Scan(&channel, &id); e != nil {
			rows.Close()
			return nil, e
		}
		links[channel] = id
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return nil, e
	}
	var wb, wbArticles, ozon []string
	e = tx.QueryRow(ctx, `SELECT wb_nm_ids,wb_articles,ozon_articles FROM canonical_product_directory WHERE variant_id=$1`, variantID).Scan(&wb, &wbArticles, &ozon)
	if e != nil {
		return nil, e
	}
	if len(wb) == 0 {
		wb = wbArticles
	}
	for channel, ids := range map[string][]string{"wb": wb, "ozon": ozon} {
		if links[channel] != "" {
			continue
		}
		if len(ids) > 1 {
			links[channel] = "!ambiguous"
		} else if len(ids) == 1 {
			links[channel] = ids[0]
		}
	}
	return links, nil
}

func editorLinkedArticle(ctx context.Context, tx pgx.Tx, variantID int64, channel, mode string) (string, error) {
	var article string
	e := tx.QueryRow(ctx, `SELECT offer_id FROM catalog_editor_remote_links WHERE variant_id=$1 AND channel=$2 AND execution_mode=$3`, variantID, channel, mode).Scan(&article)
	if e == nil {
		return article, nil
	}
	if e != pgx.ErrNoRows {
		return "", e
	}
	var articles []string
	field := "wb_articles"
	if channel == "ozon" {
		field = "ozon_articles"
	}
	e = tx.QueryRow(ctx, `SELECT `+field+` FROM canonical_product_directory WHERE variant_id=$1`, variantID).Scan(&articles)
	if len(articles) == 1 {
		article = articles[0]
	}
	return article, e
}
