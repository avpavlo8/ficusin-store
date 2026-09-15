package admin

import (
	"context"
	"fmt"
)

type ProductRelationshipMapping struct { VariantID int64 `json:"variantId"`; SKU string `json:"sku"`; Provider string `json:"provider"`; Type string `json:"type"`; ExternalID string `json:"externalId"`; Status string `json:"status"` }
type ProductSupplier struct { ID int64 `json:"id"`; Name string `json:"name"`; Article string `json:"article"`; Availability string `json:"availability"` }
type ProductChannelSales struct { Channel string `json:"channel"`; Units int `json:"units"`; Gross float64 `json:"gross"` }
type ProductRelationships struct { Mappings []ProductRelationshipMapping `json:"mappings"`; Suppliers []ProductSupplier `json:"suppliers"`; Sales []ProductChannelSales `json:"sales"` }

// ProductRelationships joins the master card to every SKU identity, supplier
// offer and recent channel sale. Missing optional channels remain absent data.
func (repository *PostgresRepository) ProductRelationships(ctx context.Context, productID int64) (ProductRelationships,error) {
	result:=ProductRelationships{Mappings:[]ProductRelationshipMapping{},Suppliers:[]ProductSupplier{},Sales:[]ProductChannelSales{}}
	rows,err:=repository.pool.Query(ctx,`SELECT e.variant_id,v.sku,e.provider,e.id_type,e.external_id,e.status FROM product_external_ids e JOIN product_variants v ON v.id=e.variant_id WHERE v.product_id=$1 ORDER BY v.id,e.provider,e.id_type,e.status,e.id`,productID);if err!=nil{return result,fmt.Errorf("product mappings: %w",err)}
	for rows.Next(){var item ProductRelationshipMapping;if err:=rows.Scan(&item.VariantID,&item.SKU,&item.Provider,&item.Type,&item.ExternalID,&item.Status);err!=nil{rows.Close();return result,err};result.Mappings=append(result.Mappings,item)};if err:=rows.Err();err!=nil{rows.Close();return result,err};rows.Close()
	rows,err=repository.pool.Query(ctx,`SELECT DISTINCT supplier.id,supplier.name,offer.supplier_article,offer.availability_status FROM (SELECT supplier_id,canonical_variant_id,supplier_article,availability_status FROM procurement_supplier_products UNION ALL SELECT supplier_id,canonical_variant_id,supplier_article,availability_status FROM procurement_supplier_aliases WHERE match_status='confirmed') offer JOIN procurement_suppliers supplier ON supplier.id=offer.supplier_id JOIN product_variants variant ON variant.id=offer.canonical_variant_id WHERE variant.product_id=$1 ORDER BY supplier.name,offer.supplier_article`,productID);if err!=nil{return result,fmt.Errorf("product suppliers: %w",err)}
	for rows.Next(){var item ProductSupplier;if err:=rows.Scan(&item.ID,&item.Name,&item.Article,&item.Availability);err!=nil{rows.Close();return result,err};result.Suppliers=append(result.Suppliers,item)};if err:=rows.Err();err!=nil{rows.Close();return result,err};rows.Close()
	rows,err=repository.pool.Query(ctx,`SELECT sale.channel,COALESCE(SUM(sale.units),0)::INTEGER,COALESCE(SUM(sale.gross_rub),0)::DOUBLE PRECISION FROM procurement_sales_daily sale JOIN product_variants variant ON variant.id=sale.canonical_variant_id WHERE variant.product_id=$1 AND sale.sale_date>=CURRENT_DATE-INTERVAL '60 days' GROUP BY sale.channel ORDER BY sale.channel`,productID);if err!=nil{return result,fmt.Errorf("product sales: %w",err)}
	for rows.Next(){var item ProductChannelSales;if err:=rows.Scan(&item.Channel,&item.Units,&item.Gross);err!=nil{rows.Close();return result,err};result.Sales=append(result.Sales,item)};if err:=rows.Err();err!=nil{rows.Close();return result,err};rows.Close();return result,nil
}
