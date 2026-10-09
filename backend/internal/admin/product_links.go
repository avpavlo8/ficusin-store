package admin

import (
	"context"
	"fmt"
	"strings"
)

// ChannelLink represents identifiers attached to a catalogue card or variant.
// Linked is derived from identifiers so clients cannot mistake an empty mapping
// for a configured marketplace connection.
type ChannelLink struct {
	Linked       bool     `json:"linked"`
	ExternalIDs  []string `json:"externalIds"`
	ListingNames []string `json:"listingNames,omitempty"`
}

type VariantLinks struct {
	ID               int64                  `json:"id"`
	SKU              string                 `json:"sku"`
	Label            string                 `json:"label"`
	SitePriceRub     float64                `json:"sitePriceRub"`
	ChannelPricesRub map[string]*float64    `json:"channelPricesRub"`
	Channels         map[string]ChannelLink `json:"channels"`
}

type ProductLinks struct {
	ID       int64                  `json:"id"`
	Name     string                 `json:"name"`
	Slug     string                 `json:"slug"`
	Status   string                 `json:"status"`
	Variants []VariantLinks         `json:"variants"`
	Channels map[string]ChannelLink `json:"channels"`
}

func emptyChannelLinks() map[string]ChannelLink {
	return map[string]ChannelLink{
		"saby":  {ExternalIDs: []string{}},
		"wb":    {ExternalIDs: []string{}},
		"ozon":  {ExternalIDs: []string{}},
		"avito": {ExternalIDs: []string{}, ListingNames: []string{}},
	}
}

func appendLink(channels map[string]ChannelLink, channel, externalID, title string) {
	externalID = strings.TrimSpace(externalID)
	if externalID == "" {
		return
	}
	link := channels[channel]
	for _, value := range link.ExternalIDs {
		if value == externalID {
			return
		}
	}
	link.Linked = true
	link.ExternalIDs = append(link.ExternalIDs, externalID)
	if title != "" {
		link.ListingNames = append(link.ListingNames, title)
	}
	channels[channel] = link
}

func externalIDChannel(provider string) string {
	switch strings.ToLower(provider) {
	case "saby":
		return "saby"
	case "wb", "wildberries":
		return "wb"
	case "ozon":
		return "ozon"
	default:
		return ""
	}
}

// ListProductLinks is intentionally read-only. The catalogue is the starting
// point; marketplace and Saby identities are reported as mappings to it.
func (repository *PostgresRepository) ListProductLinks(ctx context.Context) ([]ProductLinks, error) {
	rows, err := repository.pool.Query(ctx, `SELECT id,name,slug,status,COALESCE(saby_id,'') FROM products ORDER BY name,id`)
	if err != nil {
		return nil, fmt.Errorf("list products for links: %w", err)
	}
	products := make([]ProductLinks, 0)
	byProduct := make(map[int64]int)
	for rows.Next() {
		var product ProductLinks
		var sabyID string
		if err := rows.Scan(&product.ID, &product.Name, &product.Slug, &product.Status, &sabyID); err != nil {
			rows.Close()
			return nil, err
		}
		product.Variants = []VariantLinks{}
		product.Channels = emptyChannelLinks()
		appendLink(product.Channels, "saby", sabyID, "")
		byProduct[product.ID] = len(products)
		products = append(products, product)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()

	rows, err = repository.pool.Query(ctx, `SELECT v.id,v.product_id,v.sku,v.label,COALESCE(v.saby_id,''),
		COALESCE(pc.wb_nm_id::text,''),COALESCE(pc.wb_article,''),
		COALESCE(pc.ozon_offer_id,''),COALESCE(pc.ozon_article,''),
		v.base_price_minor::DOUBLE PRECISION/100,n.price_minor::DOUBLE PRECISION/100,
		wb_product.current_price::DOUBLE PRECISION,ozon_product.current_price::DOUBLE PRECISION
		FROM product_variants v LEFT JOIN procurement_product_channels pc ON pc.saby_id=v.saby_id
		LEFT JOIN saby_nomenclature n ON n.saby_id=v.saby_id
		LEFT JOIN procurement_channel_products wb_product ON wb_product.channel='wb' AND wb_product.external_id=pc.wb_nm_id::text
		LEFT JOIN procurement_channel_products ozon_product ON ozon_product.channel='ozon' AND ozon_product.external_id=pc.ozon_offer_id
		WHERE v.is_active<>0 AND v.archived_at IS NULL ORDER BY v.product_id,v.id`)
	if err != nil {
		return nil, fmt.Errorf("list active variant links: %w", err)
	}
	byVariant := make(map[int64][2]int)
	for rows.Next() {
		var variant VariantLinks
		var productID int64
		var sabyID, wbID, wbArticle, ozonID, ozonArticle string
		var sabyPrice, wbPrice, ozonPrice *float64
		if err := rows.Scan(&variant.ID, &productID, &variant.SKU, &variant.Label, &sabyID, &wbID, &wbArticle, &ozonID, &ozonArticle,
			&variant.SitePriceRub, &sabyPrice, &wbPrice, &ozonPrice); err != nil {
			rows.Close()
			return nil, err
		}
		productIndex, ok := byProduct[productID]
		if !ok {
			continue
		}
		variant.Channels = emptyChannelLinks()
		variant.ChannelPricesRub = map[string]*float64{"saby": sabyPrice, "wb": wbPrice, "ozon": ozonPrice}
		for _, item := range [][2]string{{"saby", sabyID}, {"wb", wbID}, {"wb", wbArticle}, {"ozon", ozonID}, {"ozon", ozonArticle}} {
			appendLink(variant.Channels, item[0], item[1], "")
			appendLink(products[productIndex].Channels, item[0], item[1], "")
		}
		byVariant[variant.ID] = [2]int{productIndex, len(products[productIndex].Variants)}
		products[productIndex].Variants = append(products[productIndex].Variants, variant)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()

	rows, err = repository.pool.Query(ctx, `SELECT product_id,variant_id,provider,external_id FROM product_external_ids WHERE status='active' ORDER BY product_id,variant_id,provider,id`)
	if err != nil {
		return nil, fmt.Errorf("list active external IDs: %w", err)
	}
	for rows.Next() {
		var productID int64
		var variantID *int64
		var provider, externalID string
		if err := rows.Scan(&productID, &variantID, &provider, &externalID); err != nil {
			rows.Close()
			return nil, err
		}
		channel := externalIDChannel(provider)
		if channel == "" {
			continue
		}
		productIndex, ok := byProduct[productID]
		if !ok {
			continue
		}
		if variantID != nil {
			indices, ok := byVariant[*variantID]
			if !ok || indices[0] != productIndex {
				continue
			}
			appendLink(products[productIndex].Variants[indices[1]].Channels, channel, externalID, "")
		}
		appendLink(products[productIndex].Channels, channel, externalID, "")
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()

	rows, err = repository.pool.Query(ctx, `SELECT lp.product_id,l.item_id,l.title FROM avito_listing_products lp
		JOIN avito_listings l ON l.item_id=lp.item_id WHERE LOWER(l.status)<>'removed' ORDER BY lp.product_id,l.item_id`)
	if err != nil {
		return nil, fmt.Errorf("list Avito links: %w", err)
	}
	for rows.Next() {
		var productID int64
		var itemID, title string
		if err := rows.Scan(&productID, &itemID, &title); err != nil {
			rows.Close()
			return nil, err
		}
		if index, ok := byProduct[productID]; ok {
			appendLink(products[index].Channels, "avito", itemID, title)
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()
	return products, nil
}
