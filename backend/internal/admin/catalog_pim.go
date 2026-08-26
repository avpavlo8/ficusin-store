package admin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/jackc/pgx/v5"
)

var attributeCodePattern = regexp.MustCompile(`^[a-z][a-z0-9_-]{1,63}$`)

type AttributeOption struct {
	ID        int64  `json:"id"`
	Code      string `json:"code"`
	Label     string `json:"label"`
	SortOrder int    `json:"sortOrder"`
	Active    bool   `json:"active"`
}

type AttributeDefinition struct {
	ID          int64             `json:"id"`
	Code        string            `json:"code"`
	Name        string            `json:"name"`
	Description string            `json:"description"`
	DataType    string            `json:"dataType"`
	Unit        string            `json:"unit"`
	Audience    string            `json:"audience"`
	Scope       string            `json:"scope"`
	Global      bool              `json:"global"`
	Active      bool              `json:"active"`
	Options     []AttributeOption `json:"options"`
}

type AttributeDefinitionInput struct {
	Code        string            `json:"code"`
	Name        string            `json:"name"`
	Description string            `json:"description"`
	DataType    string            `json:"dataType"`
	Unit        string            `json:"unit"`
	Audience    string            `json:"audience"`
	Scope       string            `json:"scope"`
	Global      bool              `json:"global"`
	Active      bool              `json:"active"`
	Options     []AttributeOption `json:"options"`
}

type EffectiveCategoryAttribute struct {
	AttributeDefinition
	Required              bool   `json:"required"`
	Filterable            bool   `json:"filterable"`
	ShowOnPDP             bool   `json:"showOnPdp"`
	KeyCharacteristic     bool   `json:"keyCharacteristic"`
	Badge                 bool   `json:"badge"`
	SortOrder             int    `json:"sortOrder"`
	SummaryPosition       *int   `json:"summaryPosition"`
	ShowInCharacteristics bool   `json:"showInCharacteristics"`
	Excluded              bool   `json:"excluded"`
	Inherited             bool   `json:"inherited"`
	SourceCategoryID      *int64 `json:"sourceCategoryId"`
	SourceCategoryName    string `json:"sourceCategoryName"`
}

type CategoryAttributeInput struct {
	AttributeID          int64 `json:"attributeId"`
	Required             bool  `json:"required"`
	Filterable           bool  `json:"filterable"`
	ShowOnPDP            bool  `json:"showOnPdp"`
	KeyCharacteristic    bool  `json:"keyCharacteristic"`
	Badge                bool  `json:"badge"`
	SortOrder            int   `json:"sortOrder"`
	SummaryPosition      *int  `json:"summaryPosition"`
	ShowInCharacteristics bool `json:"showInCharacteristics"`
	Excluded             bool  `json:"excluded"`
}

type AdminVariant struct {
	ID                 int64          `json:"id"`
	ProductID          int64          `json:"productId"`
	SKU                string         `json:"sku"`
	Label              string         `json:"label"`
	Price              float64        `json:"price"`
	Stock              int            `json:"stock"`
	WholesaleMinQty    int            `json:"wholesaleMinQty"`
	Active             bool           `json:"active"`
	Archived           bool           `json:"archived"`
	Attributes         map[string]any `json:"attributes"`
	ExternalIDs        []ExternalID   `json:"externalIds"`
	Images             []string       `json:"images"`
	ManagedFields      []string       `json:"managedFields"`
}

type VariantInput struct {
	Label           string         `json:"label"`
	PriceMinor      int64          `json:"priceMinor"`
	Stock           int            `json:"stock"`
	WholesaleMinQty int            `json:"wholesaleMinQty"`
	Active          bool           `json:"active"`
	Attributes      map[string]any `json:"attributes"`
	ExternalIDs     []ExternalID   `json:"externalIds"`
}

type CatalogFilter struct {
	ID          int64  `json:"id"`
	Code        string `json:"code"`
	Title       string `json:"title"`
	AttributeID int64  `json:"attributeId"`
	AttributeCode string `json:"attributeCode"`
	CategoryID  *int64 `json:"categoryId"`
	DisplayMode string `json:"displayMode"`
	SortOrder   int    `json:"sortOrder"`
	Active      bool   `json:"active"`
}

type CatalogFilterInput struct {
	Code        string `json:"code"`
	Title       string `json:"title"`
	AttributeID int64  `json:"attributeId"`
	CategoryID  *int64 `json:"categoryId"`
	DisplayMode string `json:"displayMode"`
	SortOrder   int    `json:"sortOrder"`
	Active      bool   `json:"active"`
}

func ownerOnly(actor Actor) error {
	if actor.Role != RoleOwner {
		return ErrForbidden
	}
	return nil
}

func validateAttributeDefinition(input AttributeDefinitionInput) error {
	input.Code = strings.TrimSpace(input.Code)
	input.Name = strings.TrimSpace(input.Name)
	if input.Code == "" || input.Name == "" {
		return fmt.Errorf("%w: code и название атрибута обязательны", ErrInvalidInput)
	}
	if !attributeCodePattern.MatchString(input.Code) {
		return fmt.Errorf("%w: code должен начинаться с латинской буквы и содержать только a-z, 0-9, _ или -", ErrInvalidInput)
	}
	if input.DataType != "text" && input.DataType != "string" && input.DataType != "number" && input.DataType != "boolean" && input.DataType != "enum" && input.DataType != "multi_enum" {
		return fmt.Errorf("%w: неизвестный тип атрибута", ErrInvalidInput)
	}
	if input.Audience != "customer" && input.Audience != "technical" {
		return fmt.Errorf("%w: неизвестная аудитория атрибута", ErrInvalidInput)
	}
	if input.Scope != "product" && input.Scope != "variant" {
		return fmt.Errorf("%w: неизвестный scope атрибута", ErrInvalidInput)
	}
	if (input.DataType == "enum" || input.DataType == "multi_enum") && len(input.Options) == 0 {
		return fmt.Errorf("%w: для enum нужен хотя бы один вариант", ErrInvalidInput)
	}
	return nil
}

func validateAttributeDefinitionChange(oldCode, oldDataType, oldScope string, input AttributeDefinitionInput, hasValues, usedByCollection bool) error {
	if hasValues && (oldDataType != input.DataType || oldScope != input.Scope) {
		return fmt.Errorf("%w: тип или scope заполненного атрибута нельзя менять; создайте новый атрибут и перенесите значения", ErrInvalidInput)
	}
	if oldCode != strings.ToLower(strings.TrimSpace(input.Code)) && usedByCollection {
		return fmt.Errorf("%w: code используется в правиле подборки; сначала переведите правило на новый атрибут", ErrInvalidInput)
	}
	return nil
}

func (repository *PostgresRepository) ListAttributeDefinitions(ctx context.Context) ([]AttributeDefinition, error) {
	rows, err := repository.pool.Query(ctx, `
		SELECT id,code,name,description,data_type,unit,audience,value_scope,is_global,is_active
		FROM attribute_definitions ORDER BY name,id
	`)
	if err != nil { return nil, fmt.Errorf("list attribute definitions: %w", err) }
	defer rows.Close()
	items := make([]AttributeDefinition,0)
	for rows.Next() {
		var item AttributeDefinition
		if err := rows.Scan(&item.ID,&item.Code,&item.Name,&item.Description,&item.DataType,&item.Unit,&item.Audience,&item.Scope,&item.Global,&item.Active); err != nil { return nil, err }
		item.Options=[]AttributeOption{}
		items=append(items,item)
	}
	if err:=rows.Err();err!=nil{return nil,err}
	byID := make(map[int64]int, len(items))
	for index := range items { byID[items[index].ID] = index }
	optionRows,err:=repository.pool.Query(ctx,`SELECT id,attribute_id,code,label,sort_order,is_active FROM attribute_options ORDER BY attribute_id,sort_order,id`)
	if err!=nil{return nil,fmt.Errorf("list attribute options: %w",err)}
	defer optionRows.Close()
	for optionRows.Next(){var option AttributeOption;var attributeID int64;if err:=optionRows.Scan(&option.ID,&attributeID,&option.Code,&option.Label,&option.SortOrder,&option.Active);err!=nil{return nil,err};if index,ok:=byID[attributeID];ok{items[index].Options=append(items[index].Options,option)}}
	return items,optionRows.Err()
}

func replaceAttributeOptions(ctx context.Context, tx pgx.Tx, attributeID int64, options []AttributeOption) error {
	seen:=map[string]bool{}
	for index,option:=range options{
		code:=strings.TrimSpace(option.Code);label:=strings.TrimSpace(option.Label)
		if code==""||label==""||seen[code]{return fmt.Errorf("%w: варианты enum должны иметь уникальные code и название",ErrInvalidInput)}
		seen[code]=true;sortOrder:=option.SortOrder;if sortOrder==0{sortOrder=(index+1)*10}
		if _,err:=tx.Exec(ctx,`INSERT INTO attribute_options(attribute_id,code,label,sort_order,is_active) VALUES($1,$2,$3,$4,$5) ON CONFLICT(attribute_id,code) DO UPDATE SET label=EXCLUDED.label,sort_order=EXCLUDED.sort_order,is_active=EXCLUDED.is_active`,attributeID,code,label,sortOrder,option.Active);err!=nil{return err}
	}
	codes:=make([]string,0,len(seen));for code:=range seen{codes=append(codes,code)}
	if _,err:=tx.Exec(ctx,`UPDATE attribute_options SET is_active=FALSE WHERE attribute_id=$1 AND NOT (code=ANY($2::text[]))`,attributeID,codes);err!=nil{return err}
	return nil
}

func (repository *PostgresRepository) CreateAttributeDefinition(ctx context.Context, actor Actor, input AttributeDefinitionInput) (AttributeDefinition,error) {
	if err:=ownerOnly(actor);err!=nil{return AttributeDefinition{},err};if err:=validateAttributeDefinition(input);err!=nil{return AttributeDefinition{},err}
	tx,err:=repository.pool.Begin(ctx);if err!=nil{return AttributeDefinition{},err};defer func(){_ = tx.Rollback(ctx)}()
	active:=input.Active
	var id int64
	err=tx.QueryRow(ctx,`INSERT INTO attribute_definitions(code,name,description,data_type,unit,audience,value_scope,is_global,is_active) VALUES(LOWER(BTRIM($1)),BTRIM($2),BTRIM($3),$4,BTRIM($5),$6,$7,$8,$9) RETURNING id`,input.Code,input.Name,input.Description,input.DataType,input.Unit,input.Audience,input.Scope,input.Global,active).Scan(&id)
	if err!=nil{return AttributeDefinition{},fmt.Errorf("create attribute: %w",err)}
	if input.DataType=="enum"||input.DataType=="multi_enum"{if err:=replaceAttributeOptions(ctx,tx,id,input.Options);err!=nil{return AttributeDefinition{},err}}
	if err:=insertAudit(ctx,tx,actor,"catalog.attribute.create","attribute",fmt.Sprint(id),nil,map[string]any{"code":input.Code});err!=nil{return AttributeDefinition{},err}
	if err:=tx.Commit(ctx);err!=nil{return AttributeDefinition{},err}
	items,err:=repository.ListAttributeDefinitions(ctx);if err!=nil{return AttributeDefinition{},err};for _,item:=range items{if item.ID==id{return item,nil}};return AttributeDefinition{},pgx.ErrNoRows
}

func (repository *PostgresRepository) UpdateAttributeDefinition(ctx context.Context, actor Actor, id int64, input AttributeDefinitionInput) (AttributeDefinition,error) {
	if err:=ownerOnly(actor);err!=nil{return AttributeDefinition{},err};if err:=validateAttributeDefinition(input);err!=nil{return AttributeDefinition{},err}
	tx,err:=repository.pool.Begin(ctx);if err!=nil{return AttributeDefinition{},err};defer func(){_ = tx.Rollback(ctx)}()
	var oldCode,oldDataType,oldScope string
	if err=tx.QueryRow(ctx,`SELECT code,data_type,value_scope FROM attribute_definitions WHERE id=$1 FOR UPDATE`,id).Scan(&oldCode,&oldDataType,&oldScope);err!=nil{return AttributeDefinition{},err}
	var hasValues bool
	if err=tx.QueryRow(ctx,`SELECT EXISTS(SELECT 1 FROM product_attribute_values WHERE attribute_id=$1 UNION ALL SELECT 1 FROM variant_attribute_values WHERE attribute_id=$1)`,id).Scan(&hasValues);err!=nil{return AttributeDefinition{},err}
	usedByCollection:=false
	if oldCode!=strings.ToLower(strings.TrimSpace(input.Code)) {
		if err=tx.QueryRow(ctx,`SELECT EXISTS(SELECT 1 FROM collection_definitions WHERE rules @> jsonb_build_array(jsonb_build_object('attribute',$1)))`,oldCode).Scan(&usedByCollection);err!=nil{return AttributeDefinition{},err}
	}
	if err=validateAttributeDefinitionChange(oldCode,oldDataType,oldScope,input,hasValues,usedByCollection);err!=nil{return AttributeDefinition{},err}
	// code, тип и уровень — контракт. Правила динамических подборок ссылаются
	// на атрибут по code, фильтры и витрина — по типу и уровню, поэтому смена
	// любого из трёх после того, как значения заполнены, молча опустошает
	// подборки и делает уже сохранённые значения нечитаемыми.
	var currentCode, currentType, currentScope string
	if err:=tx.QueryRow(ctx,`SELECT code,data_type,value_scope FROM attribute_definitions WHERE id=$1`,id).Scan(&currentCode,&currentType,&currentScope);err!=nil{return AttributeDefinition{},err}
	if currentCode!=strings.ToLower(strings.TrimSpace(input.Code))||currentType!=input.DataType||currentScope!=input.Scope{
		var used bool
		if err:=tx.QueryRow(ctx,`SELECT EXISTS(SELECT 1 FROM product_attribute_values WHERE attribute_id=$1) OR EXISTS(SELECT 1 FROM variant_attribute_values WHERE attribute_id=$1)`,id).Scan(&used);err!=nil{return AttributeDefinition{},err}
		if used{
			return AttributeDefinition{},fmt.Errorf("%w: у атрибута уже есть заполненные значения — code, тип и уровень менять нельзя. Заведите новый атрибут",ErrInvalidInput)
		}
	}
	tag,err:=tx.Exec(ctx,`UPDATE attribute_definitions SET code=LOWER(BTRIM($2)),name=BTRIM($3),description=BTRIM($4),data_type=$5,unit=BTRIM($6),audience=$7,value_scope=$8,is_global=$9,is_active=$10,updated_at=CURRENT_TIMESTAMP WHERE id=$1`,id,input.Code,input.Name,input.Description,input.DataType,input.Unit,input.Audience,input.Scope,input.Global,input.Active)
	if err!=nil{return AttributeDefinition{},fmt.Errorf("update attribute: %w",err)};if tag.RowsAffected()!=1{return AttributeDefinition{},pgx.ErrNoRows}
	if input.DataType=="enum"||input.DataType=="multi_enum"{if err:=replaceAttributeOptions(ctx,tx,id,input.Options);err!=nil{return AttributeDefinition{},err}}else{if _,err:=tx.Exec(ctx,`UPDATE attribute_options SET is_active=FALSE WHERE attribute_id=$1`,id);err!=nil{return AttributeDefinition{},err}}
	if err:=insertAudit(ctx,tx,actor,"catalog.attribute.update","attribute",fmt.Sprint(id),nil,map[string]any{"code":input.Code});err!=nil{return AttributeDefinition{},err};if err:=tx.Commit(ctx);err!=nil{return AttributeDefinition{},err}
	items,err:=repository.ListAttributeDefinitions(ctx);if err!=nil{return AttributeDefinition{},err};for _,item:=range items{if item.ID==id{return item,nil}};return AttributeDefinition{},pgx.ErrNoRows
}

func (repository *PostgresRepository) ArchiveAttributeDefinition(ctx context.Context, actor Actor, id int64) error {
	if err:=ownerOnly(actor);err!=nil{return err}
	// Архивирование выключает атрибут молча: фильтр остаётся в списке, но
	// перестаёт отдавать значения, а динамическая подборка, которая ссылается
	// на атрибут по code, начинает возвращать пусто для всех товаров. Пусть
	// владелец сначала увидит, что именно сломается.
	var filters int
	if err:=repository.pool.QueryRow(ctx,`SELECT COUNT(*) FROM catalog_filters WHERE attribute_id=$1 AND is_active`,id).Scan(&filters);err!=nil{return err}
	collections:=[]string{}
	rows,err:=repository.pool.Query(ctx,`
		SELECT collection.title FROM collections collection
		JOIN attribute_definitions definition ON definition.id=$1
		WHERE collection.is_active=1 AND collection.mode='dynamic'
		  AND EXISTS(SELECT 1 FROM jsonb_array_elements(collection.rules) rule WHERE BTRIM(rule->>'attribute')=definition.code)
		ORDER BY collection.title
	`,id);if err!=nil{return err}
	for rows.Next(){var title string;if err:=rows.Scan(&title);err!=nil{rows.Close();return err};collections=append(collections,title)}
	rows.Close();if err:=rows.Err();err!=nil{return err}
	if filters>0||len(collections)>0{
		detail:=fmt.Sprintf("активных фильтров: %d",filters)
		if len(collections)>0{detail+="; подборки: "+strings.Join(collections,", ")}
		return fmt.Errorf("%w: атрибут используется на витрине (%s). Сначала отключите их",ErrInvalidInput,detail)
	}
	tag,err:=repository.pool.Exec(ctx,`UPDATE attribute_definitions SET is_active=FALSE,updated_at=CURRENT_TIMESTAMP WHERE id=$1`,id);if err!=nil{return err};if tag.RowsAffected()!=1{return pgx.ErrNoRows};return nil
}

func (repository *PostgresRepository) EffectiveCategoryAttributes(ctx context.Context, categoryID int64) ([]EffectiveCategoryAttribute,error) {
	rows,err:=repository.pool.Query(ctx,`
		WITH RECURSIVE ancestors AS (
			SELECT id,parent_id,0 depth FROM categories WHERE id=$1
			UNION ALL SELECT c.id,c.parent_id,a.depth+1 FROM categories c JOIN ancestors a ON a.parent_id=c.id
		), candidates AS (
			SELECT d.id,d.code,d.name,d.description,d.data_type,d.unit,d.audience,d.value_scope,d.is_global,d.is_active,
				ca.is_required,ca.is_filterable,ca.show_on_pdp,ca.show_in_summary,ca.is_badge,ca.sort_order,ca.summary_position,
				ca.show_in_characteristics,ca.is_excluded,a.depth,c.id source_category_id,c.name source_category_name
			FROM ancestors a JOIN categories c ON c.id=a.id JOIN category_attributes ca ON ca.category_id=a.id
			JOIN attribute_definitions d ON d.id=ca.attribute_id
			UNION ALL
			SELECT d.id,d.code,d.name,d.description,d.data_type,d.unit,d.audience,d.value_scope,d.is_global,d.is_active,
				FALSE,FALSE,TRUE,FALSE,FALSE,1000,NULL,TRUE,FALSE,1000000,NULL::BIGINT,''
			FROM attribute_definitions d WHERE d.is_global
		), effective AS (
			SELECT DISTINCT ON(id) * FROM candidates ORDER BY id,depth
		)
		SELECT id,code,name,description,data_type,unit,audience,value_scope,is_global,is_active,
			is_required,is_filterable,show_on_pdp,show_in_summary,is_badge,sort_order,summary_position,
			show_in_characteristics,is_excluded,source_category_id,source_category_name,
			COALESCE(source_category_id<>$1,FALSE)
		FROM effective ORDER BY sort_order,name,id
	`,categoryID)
	if err!=nil{return nil,fmt.Errorf("effective category attributes: %w",err)};defer rows.Close();items:=[]EffectiveCategoryAttribute{}
	for rows.Next(){var item EffectiveCategoryAttribute;if err:=rows.Scan(&item.ID,&item.Code,&item.Name,&item.Description,&item.DataType,&item.Unit,&item.Audience,&item.Scope,&item.Global,&item.Active,&item.Required,&item.Filterable,&item.ShowOnPDP,&item.KeyCharacteristic,&item.Badge,&item.SortOrder,&item.SummaryPosition,&item.ShowInCharacteristics,&item.Excluded,&item.SourceCategoryID,&item.SourceCategoryName,&item.Inherited);err!=nil{return nil,err};item.Options=[]AttributeOption{};items=append(items,item)}
	if err:=rows.Err();err!=nil{return nil,err}
	definitions,err:=repository.ListAttributeDefinitions(ctx);if err!=nil{return nil,err};options:=map[int64][]AttributeOption{};for _,definition:=range definitions{options[definition.ID]=definition.Options};for index:=range items{items[index].Options=options[items[index].ID]}
	return items,nil
}

func (repository *PostgresRepository) SetCategoryAttribute(ctx context.Context, actor Actor, categoryID int64, input CategoryAttributeInput) error {
	if err:=ownerOnly(actor);err!=nil{return err};if categoryID<=0||input.AttributeID<=0{return ErrInvalidInput}
	if input.Excluded && (input.Required || input.Filterable || input.ShowOnPDP || input.KeyCharacteristic || input.Badge || input.ShowInCharacteristics) {
		return fmt.Errorf("%w: исключённый атрибут не может одновременно отображаться или быть обязательным", ErrInvalidInput)
	}
	if input.Filterable || input.ShowOnPDP || input.KeyCharacteristic || input.Badge || input.ShowInCharacteristics {
		var audience string
		if err := repository.pool.QueryRow(ctx, `SELECT audience FROM attribute_definitions WHERE id=$1 AND is_active`, input.AttributeID).Scan(&audience); err != nil { return err }
		if audience != "customer" { return fmt.Errorf("%w: технический атрибут нельзя выводить на витрину", ErrInvalidInput) }
	}
	_,err:=repository.pool.Exec(ctx,`INSERT INTO category_attributes(category_id,attribute_id,is_required,is_filterable,show_on_pdp,is_badge,sort_order,show_in_summary,summary_position,show_in_characteristics,is_excluded) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11) ON CONFLICT(category_id,attribute_id) DO UPDATE SET is_required=EXCLUDED.is_required,is_filterable=EXCLUDED.is_filterable,show_on_pdp=EXCLUDED.show_on_pdp,is_badge=EXCLUDED.is_badge,sort_order=EXCLUDED.sort_order,show_in_summary=EXCLUDED.show_in_summary,summary_position=EXCLUDED.summary_position,show_in_characteristics=EXCLUDED.show_in_characteristics,is_excluded=EXCLUDED.is_excluded`,categoryID,input.AttributeID,input.Required,input.Filterable,input.ShowOnPDP,input.Badge,input.SortOrder,input.KeyCharacteristic,input.SummaryPosition,input.ShowInCharacteristics,input.Excluded)
	return err
}

func (repository *PostgresRepository) RemoveCategoryAttribute(ctx context.Context, actor Actor, categoryID, attributeID int64) error {
	if err:=ownerOnly(actor);err!=nil{return err};_,err:=repository.pool.Exec(ctx,`DELETE FROM category_attributes WHERE category_id=$1 AND attribute_id=$2`,categoryID,attributeID);return err
}

func decodeJSONMap(raw []byte) map[string]any { result:=map[string]any{};_ = json.Unmarshal(raw,&result);return result }
func decodeExternalIDs(raw []byte) []ExternalID { result:=[]ExternalID{};_ = json.Unmarshal(raw,&result);return result }
func decodeStrings(raw []byte) []string { result:=[]string{};_ = json.Unmarshal(raw,&result);return result }

func (repository *PostgresRepository) ListProductVariants(ctx context.Context, productID int64) ([]AdminVariant,error) {
	rows,err:=repository.pool.Query(ctx,`
		SELECT v.id,v.product_id,v.sku,v.label,v.base_price_minor,
			COALESCE((SELECT SUM(GREATEST(i.available_qty-i.reserved_qty,0)) FROM inventory i WHERE i.variant_id=v.id),0)::INTEGER,
			v.wholesale_min_qty,v.is_active=1,v.archived_at IS NOT NULL,
			COALESCE((SELECT jsonb_object_agg(d.code,av.value) FROM variant_attribute_values av JOIN attribute_definitions d ON d.id=av.attribute_id WHERE av.variant_id=v.id),'{}'::jsonb),
			COALESCE((SELECT jsonb_agg(jsonb_build_object('provider',e.provider,'type',e.id_type,'externalId',e.external_id) ORDER BY e.provider,e.id_type) FROM product_external_ids e WHERE e.variant_id=v.id),'[]'::jsonb),
			COALESCE((SELECT jsonb_agg(COALESCE(mirror.large_url,m.object_key) ORDER BY m.is_primary DESC,m.sort_order,m.id) FROM product_media m LEFT JOIN media_mirror mirror ON mirror.source_url=m.object_key WHERE m.variant_id=v.id),'[]'::jsonb),p.saby_fields
		FROM product_variants v JOIN products p ON p.id=v.product_id WHERE v.product_id=$1 ORDER BY v.archived_at NULLS FIRST,v.id
	`,productID);if err!=nil{return nil,fmt.Errorf("list variants: %w",err)};defer rows.Close();items:=[]AdminVariant{}
	for rows.Next(){var item AdminVariant;var priceMinor int64;var attributes,external,images []byte;if err:=rows.Scan(&item.ID,&item.ProductID,&item.SKU,&item.Label,&priceMinor,&item.Stock,&item.WholesaleMinQty,&item.Active,&item.Archived,&attributes,&external,&images,&item.ManagedFields);err!=nil{return nil,err};item.Price=float64(priceMinor)/100;item.Attributes=decodeJSONMap(attributes);item.ExternalIDs=decodeExternalIDs(external);item.Images=decodeStrings(images);items=append(items,item)};return items,rows.Err()
}

func validateVariantInput(input VariantInput) error { if strings.TrimSpace(input.Label)==""{return fmt.Errorf("%w: название варианта обязательно",ErrInvalidInput)};if input.PriceMinor<0||input.Stock<0||input.WholesaleMinQty<1{return fmt.Errorf("%w: неверная цена, остаток или минимальное количество",ErrInvalidInput)};return nil }

func saveVariantPIMValues(ctx context.Context, tx pgx.Tx, productID,variantID int64, values map[string]any) error {
	for code,value:=range values{
		code=strings.TrimSpace(code);if code==""{continue};raw,err:=json.Marshal(value);if err!=nil{return err}
		if string(raw)=="null"||string(raw)==`""`||string(raw)=="[]"{if _,err:=tx.Exec(ctx,`DELETE FROM variant_attribute_values v USING attribute_definitions d WHERE v.attribute_id=d.id AND v.variant_id=$1 AND d.code=$2`,variantID,code);err!=nil{return err};continue}
		tag,err:=tx.Exec(ctx,`
			WITH RECURSIVE ancestors AS (
				SELECT c.id,c.parent_id,0 depth FROM products p JOIN categories c ON c.id=p.category_id WHERE p.id=$1
				UNION ALL SELECT c.id,c.parent_id,a.depth+1 FROM categories c JOIN ancestors a ON a.parent_id=c.id
			), candidates AS (
				SELECT d.id,d.data_type,d.value_scope,ca.is_excluded,a.depth
				FROM ancestors a JOIN category_attributes ca ON ca.category_id=a.id
				JOIN attribute_definitions d ON d.id=ca.attribute_id
				WHERE d.code=$3 AND d.is_active
				UNION ALL
				SELECT d.id,d.data_type,d.value_scope,FALSE,1000000 FROM attribute_definitions d
				WHERE d.code=$3 AND d.is_active AND d.is_global
			), effective AS (
				SELECT DISTINCT ON(id) id,data_type,value_scope,is_excluded FROM candidates ORDER BY id,depth
			)
			INSERT INTO variant_attribute_values(variant_id,attribute_id,value,source,updated_at)
			SELECT $2,e.id,$4::jsonb,'local',CURRENT_TIMESTAMP FROM effective e
			WHERE e.value_scope='variant' AND NOT COALESCE(e.is_excluded,FALSE)
			  AND CASE e.data_type
				WHEN 'number' THEN jsonb_typeof($4::jsonb)='number'
				WHEN 'boolean' THEN jsonb_typeof($4::jsonb)='boolean'
				WHEN 'enum' THEN jsonb_typeof($4::jsonb)='string' AND EXISTS(SELECT 1 FROM attribute_options o WHERE o.attribute_id=e.id AND o.code=($4::jsonb#>>'{}') AND o.is_active)
				WHEN 'multi_enum' THEN jsonb_typeof($4::jsonb)='array' AND NOT EXISTS(SELECT 1 FROM jsonb_array_elements_text($4::jsonb) x WHERE NOT EXISTS(SELECT 1 FROM attribute_options o WHERE o.attribute_id=e.id AND o.code=x AND o.is_active))
				ELSE jsonb_typeof($4::jsonb)='string' END
			ON CONFLICT(variant_id,attribute_id) DO UPDATE SET value=EXCLUDED.value,source='local',updated_at=CURRENT_TIMESTAMP
		`,productID,variantID,code,string(raw));if err!=nil{return fmt.Errorf("save variant attribute %s: %w",code,err)};if tag.RowsAffected()!=1{return fmt.Errorf("%w: атрибут %s не разрешён для SKU или имеет неверное значение",ErrInvalidInput,code)}
	}
	return nil
}

func validateRequiredVariantAttributes(ctx context.Context, tx pgx.Tx, productID, variantID int64) error {
	var missing string
	err := tx.QueryRow(ctx, `
		WITH RECURSIVE ancestors AS (
			SELECT c.id,c.parent_id,0 depth FROM products p JOIN categories c ON c.id=p.category_id WHERE p.id=$1
			UNION ALL SELECT c.id,c.parent_id,a.depth+1 FROM categories c JOIN ancestors a ON a.parent_id=c.id
		), candidates AS (
			SELECT d.id,d.code,ca.is_required,ca.is_excluded,a.depth
			FROM ancestors a JOIN category_attributes ca ON ca.category_id=a.id
			JOIN attribute_definitions d ON d.id=ca.attribute_id
			WHERE d.is_active AND d.value_scope='variant'
		), effective AS (
			SELECT DISTINCT ON(id) id,code,is_required,is_excluded FROM candidates ORDER BY id,depth
		)
		SELECT e.code FROM effective e
		LEFT JOIN variant_attribute_values v ON v.variant_id=$2 AND v.attribute_id=e.id
		WHERE e.is_required AND NOT e.is_excluded AND v.attribute_id IS NULL
		ORDER BY e.code LIMIT 1
	`, productID, variantID).Scan(&missing)
	if errors.Is(err, pgx.ErrNoRows) { return nil }
	if err != nil { return err }
	return fmt.Errorf("%w: обязательный атрибут SKU %s не заполнен", ErrInvalidInput, missing)
}

func replaceVariantExternalIDs(ctx context.Context,tx pgx.Tx,productID,variantID int64,values []ExternalID) error {
	if _,err:=tx.Exec(ctx,`DELETE FROM product_external_ids WHERE variant_id=$1 AND provider NOT IN ('ficusin','saby')`,variantID);err!=nil{return err}
	for _,item:=range values{provider:=strings.ToLower(strings.TrimSpace(item.Provider));kind:=strings.ToLower(strings.TrimSpace(item.Type));external:=strings.TrimSpace(item.ExternalID);if provider==""||kind==""||external==""||provider=="ficusin"||provider=="saby"{continue};if _,err:=tx.Exec(ctx,`INSERT INTO product_external_ids(product_id,variant_id,provider,id_type,external_id) VALUES($1,$2,$3,$4,$5) ON CONFLICT(provider,id_type,external_id) DO UPDATE SET product_id=EXCLUDED.product_id,variant_id=EXCLUDED.variant_id,updated_at=CURRENT_TIMESTAMP`,productID,variantID,provider,kind,external);err!=nil{return err}}
	return nil
}

func setVariantStock(ctx context.Context,tx pgx.Tx,variantID int64,stock int) error {
	if stock<0{return ErrInvalidInput};var warehouseID int64;err:=tx.QueryRow(ctx,`SELECT id FROM warehouses WHERE is_active=1 ORDER BY (saby_id='saby-ryazan-main') DESC,id LIMIT 1`).Scan(&warehouseID);if errors.Is(err,pgx.ErrNoRows){err=tx.QueryRow(ctx,`INSERT INTO warehouses(saby_id,name,city,address,is_active) VALUES('saby-ryazan-main','Основной склад','Рязань','Новосёлов, 40А',1) RETURNING id`).Scan(&warehouseID)};if err!=nil{return err};_,err=tx.Exec(ctx,`INSERT INTO inventory(warehouse_id,variant_id,available_qty,reserved_qty,synced_at) VALUES($1,$2,$3,0,CURRENT_TIMESTAMP) ON CONFLICT(warehouse_id,variant_id) DO UPDATE SET available_qty=EXCLUDED.available_qty+inventory.reserved_qty,synced_at=CURRENT_TIMESTAMP`,warehouseID,variantID,stock);return err
}

func (repository *PostgresRepository) CreateProductVariant(ctx context.Context, actor Actor, productID int64, input VariantInput) (AdminVariant,error) {
	if !Can(actor.Role,PermissionProductsEdit){return AdminVariant{},ErrForbidden};if err:=validateVariantInput(input);err!=nil{return AdminVariant{},err};tx,err:=repository.pool.Begin(ctx);if err!=nil{return AdminVariant{},err};defer func(){_ = tx.Rollback(ctx)}()
	var id int64;err=tx.QueryRow(ctx,`INSERT INTO product_variants(product_id,label,base_price_minor,wholesale_min_qty,is_active,updated_at) SELECT $1,BTRIM($2),$3,$4,$5,CURRENT_TIMESTAMP WHERE EXISTS(SELECT 1 FROM products WHERE id=$1) RETURNING id`,productID,input.Label,input.PriceMinor,input.WholesaleMinQty,boolToSmallInt(input.Active)).Scan(&id);if err!=nil{return AdminVariant{},err};if err:=setVariantStock(ctx,tx,id,input.Stock);err!=nil{return AdminVariant{},err};if err:=saveVariantPIMValues(ctx,tx,productID,id,input.Attributes);err!=nil{return AdminVariant{},err};if input.Active { if err:=validateRequiredVariantAttributes(ctx,tx,productID,id);err!=nil{return AdminVariant{},err} };if err:=replaceVariantExternalIDs(ctx,tx,productID,id,input.ExternalIDs);err!=nil{return AdminVariant{},err};if err:=tx.Commit(ctx);err!=nil{return AdminVariant{},err};items,err:=repository.ListProductVariants(ctx,productID);if err!=nil{return AdminVariant{},err};for _,item:=range items{if item.ID==id{return item,nil}};return AdminVariant{},pgx.ErrNoRows
}

func (repository *PostgresRepository) UpdateProductVariant(ctx context.Context, actor Actor, variantID int64, input VariantInput) (AdminVariant,error) {
	if !Can(actor.Role,PermissionProductsEdit){return AdminVariant{},ErrForbidden};if err:=validateVariantInput(input);err!=nil{return AdminVariant{},err};tx,err:=repository.pool.Begin(ctx);if err!=nil{return AdminVariant{},err};defer func(){_ = tx.Rollback(ctx)}();var productID int64;var sabyPrice,sabyStock bool;if err=tx.QueryRow(ctx,`SELECT v.product_id,'price'=ANY(p.saby_fields),'stock'=ANY(p.saby_fields) FROM product_variants v JOIN products p ON p.id=v.product_id WHERE v.id=$1 FOR UPDATE OF v`,variantID).Scan(&productID,&sabyPrice,&sabyStock);err!=nil{return AdminVariant{},err};err=tx.QueryRow(ctx,`UPDATE product_variants SET label=BTRIM($2),base_price_minor=CASE WHEN $6 THEN base_price_minor ELSE $3 END,wholesale_min_qty=$4,is_active=$5,archived_at=CASE WHEN $5=1 THEN NULL ELSE archived_at END,updated_at=CURRENT_TIMESTAMP WHERE id=$1 RETURNING product_id`,variantID,input.Label,input.PriceMinor,input.WholesaleMinQty,boolToSmallInt(input.Active),sabyPrice).Scan(&productID);if err!=nil{return AdminVariant{},err};if !sabyStock { if err:=setVariantStock(ctx,tx,variantID,input.Stock);err!=nil{return AdminVariant{},err} };if err:=saveVariantPIMValues(ctx,tx,productID,variantID,input.Attributes);err!=nil{return AdminVariant{},err};if input.Active { if err:=validateRequiredVariantAttributes(ctx,tx,productID,variantID);err!=nil{return AdminVariant{},err} };if err:=replaceVariantExternalIDs(ctx,tx,productID,variantID,input.ExternalIDs);err!=nil{return AdminVariant{},err};if err:=tx.Commit(ctx);err!=nil{return AdminVariant{},err};items,err:=repository.ListProductVariants(ctx,productID);if err!=nil{return AdminVariant{},err};for _,item:=range items{if item.ID==variantID{return item,nil}};return AdminVariant{},pgx.ErrNoRows
}

// CopyProductVariant отдаёт копию выключенной: активный SKU обязан иметь
// заполненные обязательные атрибуты, а копия их не проверяет. Включение
// проходит через обычный путь редактирования, где проверка есть.
func (repository *PostgresRepository) CopyProductVariant(ctx context.Context, actor Actor, variantID int64) (AdminVariant,error) {
	if !Can(actor.Role,PermissionProductsEdit){return AdminVariant{},ErrForbidden};tx,err:=repository.pool.Begin(ctx);if err!=nil{return AdminVariant{},err};defer func(){_ = tx.Rollback(ctx)}();var productID,newID int64;err=tx.QueryRow(ctx,`INSERT INTO product_variants(product_id,label,base_price_minor,price_override_minor,wholesale_min_qty,is_active,updated_at) SELECT product_id,label || ' — копия',base_price_minor,price_override_minor,wholesale_min_qty,0,CURRENT_TIMESTAMP FROM product_variants WHERE id=$1 RETURNING id,product_id`,variantID).Scan(&newID,&productID);if err!=nil{return AdminVariant{},err};if _,err:=tx.Exec(ctx,`INSERT INTO variant_attribute_values(variant_id,attribute_id,value,source,updated_at) SELECT $2,attribute_id,value,'local',CURRENT_TIMESTAMP FROM variant_attribute_values WHERE variant_id=$1`,variantID,newID);err!=nil{return AdminVariant{},err};if _,err:=tx.Exec(ctx,`INSERT INTO inventory(warehouse_id,variant_id,available_qty,reserved_qty,synced_at) SELECT warehouse_id,$2,0,0,CURRENT_TIMESTAMP FROM inventory WHERE variant_id=$1 ON CONFLICT DO NOTHING`,variantID,newID);err!=nil{return AdminVariant{},err};if err:=tx.Commit(ctx);err!=nil{return AdminVariant{},err};items,err:=repository.ListProductVariants(ctx,productID);if err!=nil{return AdminVariant{},err};for _,item:=range items{if item.ID==newID{return item,nil}};return AdminVariant{},pgx.ErrNoRows
}

func (repository *PostgresRepository) ArchiveProductVariant(ctx context.Context, actor Actor, variantID int64) error { if !Can(actor.Role,PermissionProductsEdit){return ErrForbidden};tag,err:=repository.pool.Exec(ctx,`UPDATE product_variants SET is_active=0,archived_at=CURRENT_TIMESTAMP,updated_at=CURRENT_TIMESTAMP WHERE id=$1`,variantID);if err!=nil{return err};if tag.RowsAffected()!=1{return pgx.ErrNoRows};return nil }
func (repository *PostgresRepository) DeleteProductVariant(ctx context.Context, actor Actor, variantID int64) error { if !Can(actor.Role,PermissionProductsEdit){return ErrForbidden};var sold bool;if err:=repository.pool.QueryRow(ctx,`SELECT EXISTS(SELECT 1 FROM order_items WHERE variant_id=$1)`,variantID).Scan(&sold);err!=nil{return err};if sold{return fmt.Errorf("%w: проданный SKU можно только архивировать",ErrInvalidInput)};tag,err:=repository.pool.Exec(ctx,`DELETE FROM product_variants WHERE id=$1`,variantID);if err!=nil{return err};if tag.RowsAffected()!=1{return pgx.ErrNoRows};return nil }

func (repository *PostgresRepository) ListCatalogFilters(ctx context.Context) ([]CatalogFilter,error) { rows,err:=repository.pool.Query(ctx,`SELECT f.id,f.code,f.title,f.attribute_id,d.code,f.category_id,f.display_mode,f.sort_order,f.is_active FROM catalog_filters f JOIN attribute_definitions d ON d.id=f.attribute_id ORDER BY f.sort_order,f.id`);if err!=nil{return nil,err};defer rows.Close();items:=[]CatalogFilter{};for rows.Next(){var item CatalogFilter;if err:=rows.Scan(&item.ID,&item.Code,&item.Title,&item.AttributeID,&item.AttributeCode,&item.CategoryID,&item.DisplayMode,&item.SortOrder,&item.Active);err!=nil{return nil,err};items=append(items,item)};return items,rows.Err() }
func validateFilter(input CatalogFilterInput) error { if !attributeCodePattern.MatchString(strings.TrimSpace(input.Code))||strings.TrimSpace(input.Title)==""||input.AttributeID<=0{return fmt.Errorf("%w: заполните название и корректный латинский code",ErrInvalidInput)};if input.DisplayMode!="select"&&input.DisplayMode!="chips"&&input.DisplayMode!="range"{return ErrInvalidInput};return nil }
func validateFilterAttribute(ctx context.Context, pool interface{ QueryRow(context.Context,string,...any) pgx.Row }, input CatalogFilterInput) error { var dataType,audience string;var active bool;if err:=pool.QueryRow(ctx,`SELECT data_type,audience,is_active FROM attribute_definitions WHERE id=$1`,input.AttributeID).Scan(&dataType,&audience,&active);err!=nil{return err};if !active||audience!="customer"{return fmt.Errorf("%w: фильтр доступен только для активного клиентского атрибута",ErrInvalidInput)};if input.DisplayMode=="range"&&dataType!="number"{return fmt.Errorf("%w: диапазон доступен только для числового атрибута",ErrInvalidInput)};if input.DisplayMode!="range"&&dataType=="number"{return fmt.Errorf("%w: числовой атрибут должен использовать диапазон",ErrInvalidInput)};return nil}
func (repository *PostgresRepository) CreateCatalogFilter(ctx context.Context,actor Actor,input CatalogFilterInput)(CatalogFilter,error){if err:=ownerOnly(actor);err!=nil{return CatalogFilter{},err};if err:=validateFilter(input);err!=nil{return CatalogFilter{},err};if err:=validateFilterAttribute(ctx,repository.pool,input);err!=nil{return CatalogFilter{},err};var id int64;err:=repository.pool.QueryRow(ctx,`INSERT INTO catalog_filters(code,title,attribute_id,category_id,display_mode,sort_order,is_active) VALUES(BTRIM($1),BTRIM($2),$3,$4,$5,$6,$7) RETURNING id`,input.Code,input.Title,input.AttributeID,input.CategoryID,input.DisplayMode,input.SortOrder,input.Active).Scan(&id);if err!=nil{return CatalogFilter{},err};items,err:=repository.ListCatalogFilters(ctx);if err!=nil{return CatalogFilter{},err};for _,item:=range items{if item.ID==id{return item,nil}};return CatalogFilter{},pgx.ErrNoRows}
func (repository *PostgresRepository) UpdateCatalogFilter(ctx context.Context,actor Actor,id int64,input CatalogFilterInput)(CatalogFilter,error){if err:=ownerOnly(actor);err!=nil{return CatalogFilter{},err};if err:=validateFilter(input);err!=nil{return CatalogFilter{},err};if err:=validateFilterAttribute(ctx,repository.pool,input);err!=nil{return CatalogFilter{},err};tag,err:=repository.pool.Exec(ctx,`UPDATE catalog_filters SET code=BTRIM($2),title=BTRIM($3),attribute_id=$4,category_id=$5,display_mode=$6,sort_order=$7,is_active=$8,updated_at=CURRENT_TIMESTAMP WHERE id=$1`,id,input.Code,input.Title,input.AttributeID,input.CategoryID,input.DisplayMode,input.SortOrder,input.Active);if err!=nil{return CatalogFilter{},err};if tag.RowsAffected()!=1{return CatalogFilter{},pgx.ErrNoRows};items,err:=repository.ListCatalogFilters(ctx);if err!=nil{return CatalogFilter{},err};for _,item:=range items{if item.ID==id{return item,nil}};return CatalogFilter{},pgx.ErrNoRows}
func (repository *PostgresRepository) DeleteCatalogFilter(ctx context.Context,actor Actor,id int64)error{if err:=ownerOnly(actor);err!=nil{return err};tag,err:=repository.pool.Exec(ctx,`DELETE FROM catalog_filters WHERE id=$1`,id);if err!=nil{return err};if tag.RowsAffected()!=1{return pgx.ErrNoRows};return nil}
