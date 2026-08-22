package admin

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/jackc/pgx/v5"
)

type CollectionRule struct {
	Attribute string `json:"attribute"`
	Operator  string `json:"operator"`
	Value     any    `json:"value,omitempty"`
}

type CollectionDefinition struct {
	ID        int64            `json:"id"`
	Slug      string           `json:"slug"`
	Title     string           `json:"title"`
	Note      string           `json:"note"`
	CoverURL  string           `json:"coverUrl"`
	SortOrder int              `json:"sortOrder"`
	Active    bool             `json:"active"`
	Mode      string           `json:"mode"`
	Rules     []CollectionRule `json:"rules"`
	Products  []int64          `json:"products"`
}

type CollectionDefinitionInput struct {
	Slug      string           `json:"slug"`
	Title     string           `json:"title"`
	Note      string           `json:"note"`
	CoverURL  string           `json:"coverUrl"`
	SortOrder int              `json:"sortOrder"`
	Active    bool             `json:"active"`
	Mode      string           `json:"mode"`
	Rules     []CollectionRule `json:"rules"`
}

var collectionSlugPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}$`)

func validateCollectionDefinition(input *CollectionDefinitionInput) error {
	input.Slug = strings.ToLower(strings.TrimSpace(input.Slug))
	input.Title = strings.TrimSpace(input.Title)
	input.Note = strings.TrimSpace(input.Note)
	input.CoverURL = strings.TrimSpace(input.CoverURL)
	input.Mode = strings.ToLower(strings.TrimSpace(input.Mode))
	if input.Mode == "" { input.Mode = "manual" }
	if input.Title == "" || !collectionSlugPattern.MatchString(input.Slug) {
		return fmt.Errorf("%w: заполните название и slug латиницей", ErrInvalidInput)
	}
	if input.Mode != "manual" && input.Mode != "dynamic" {
		return fmt.Errorf("%w: неизвестный режим подборки", ErrInvalidInput)
	}
	if input.Mode == "manual" {
		input.Rules = []CollectionRule{}
		return nil
	}
	if len(input.Rules) == 0 {
		return fmt.Errorf("%w: для динамической подборки нужно хотя бы одно правило", ErrInvalidInput)
	}
	allowed := map[string]bool{"eq": true, "neq": true, "in": true, "contains": true, "gte": true, "lte": true, "exists": true}
	for index := range input.Rules {
		rule := &input.Rules[index]
		rule.Attribute = strings.TrimSpace(rule.Attribute)
		rule.Operator = strings.ToLower(strings.TrimSpace(rule.Operator))
		if rule.Attribute == "" || !allowed[rule.Operator] {
			return fmt.Errorf("%w: некорректное правило подборки", ErrInvalidInput)
		}
	}
	return nil
}

func (repository *PostgresRepository) ListCollectionDefinitions(ctx context.Context) ([]CollectionDefinition, error) {
	rows, err := repository.pool.Query(ctx, `
		SELECT collection.id,collection.slug,collection.title,collection.note,collection.cover_url,collection.sort_order,
			collection.is_active<>0,collection.mode,collection.rules,
			CASE WHEN collection.mode='dynamic' THEN
				COALESCE((SELECT ARRAY_AGG(product.id ORDER BY product.id)
					FROM products product
					WHERE collection_rules_match_product(product.id,collection.rules)),ARRAY[]::BIGINT[])
			ELSE COALESCE((SELECT ARRAY_AGG(member.product_id ORDER BY member.sort_order,member.product_id)
				FROM collection_products member WHERE member.collection_id=collection.id),ARRAY[]::BIGINT[])
			END
		FROM collections collection ORDER BY collection.sort_order,collection.id
	`)
	if err != nil { return nil, fmt.Errorf("list collection definitions: %w", err) }
	defer rows.Close()
	items := make([]CollectionDefinition, 0)
	for rows.Next() {
		var item CollectionDefinition
		var raw []byte
		if err := rows.Scan(&item.ID,&item.Slug,&item.Title,&item.Note,&item.CoverURL,&item.SortOrder,&item.Active,&item.Mode,&raw,&item.Products); err != nil { return nil, err }
		if err := json.Unmarshal(raw, &item.Rules); err != nil { return nil, fmt.Errorf("decode collection rules: %w", err) }
		items = append(items, item)
	}
	return items, rows.Err()
}

func (repository *PostgresRepository) CreateCollectionDefinition(ctx context.Context, actor Actor, input CollectionDefinitionInput) (CollectionDefinition, error) {
	if !Can(actor.Role, PermissionProductsEdit) { return CollectionDefinition{}, ErrForbidden }
	if err := validateCollectionDefinition(&input); err != nil { return CollectionDefinition{}, err }
	raw, _ := json.Marshal(input.Rules)
	var id int64
	if err := repository.pool.QueryRow(ctx, `
		INSERT INTO collections(slug,title,note,cover_url,sort_order,is_active,mode,rules,updated_at)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8::jsonb,CURRENT_TIMESTAMP) RETURNING id
	`, input.Slug,input.Title,input.Note,input.CoverURL,input.SortOrder,boolToSmallInt(input.Active),input.Mode,string(raw)).Scan(&id); err != nil {
		return CollectionDefinition{}, fmt.Errorf("create collection: %w", err)
	}
	items, err := repository.ListCollectionDefinitions(ctx)
	if err != nil { return CollectionDefinition{}, err }
	for _, item := range items { if item.ID == id { return item, nil } }
	return CollectionDefinition{}, pgx.ErrNoRows
}

func (repository *PostgresRepository) UpdateCollectionDefinition(ctx context.Context, actor Actor, id int64, input CollectionDefinitionInput) (CollectionDefinition, error) {
	if !Can(actor.Role, PermissionProductsEdit) { return CollectionDefinition{}, ErrForbidden }
	if err := validateCollectionDefinition(&input); err != nil { return CollectionDefinition{}, err }
	raw, _ := json.Marshal(input.Rules)
	tag, err := repository.pool.Exec(ctx, `
		UPDATE collections SET slug=$2,title=$3,note=$4,cover_url=$5,sort_order=$6,is_active=$7,mode=$8,rules=$9::jsonb,updated_at=CURRENT_TIMESTAMP
		WHERE id=$1
	`, id,input.Slug,input.Title,input.Note,input.CoverURL,input.SortOrder,boolToSmallInt(input.Active),input.Mode,string(raw))
	if err != nil { return CollectionDefinition{}, fmt.Errorf("update collection: %w", err) }
	if tag.RowsAffected() != 1 { return CollectionDefinition{}, pgx.ErrNoRows }
	items, err := repository.ListCollectionDefinitions(ctx)
	if err != nil { return CollectionDefinition{}, err }
	for _, item := range items { if item.ID == id { return item, nil } }
	return CollectionDefinition{}, pgx.ErrNoRows
}

func (repository *PostgresRepository) DeleteCollectionDefinition(ctx context.Context, actor Actor, id int64) error {
	if !Can(actor.Role, PermissionProductsEdit) { return ErrForbidden }
	tag, err := repository.pool.Exec(ctx, `DELETE FROM collections WHERE id=$1`, id)
	if err != nil { return fmt.Errorf("delete collection: %w", err) }
	if tag.RowsAffected() != 1 { return pgx.ErrNoRows }
	return nil
}

func (repository *PostgresRepository) SetCollectionCover(ctx context.Context, actor Actor, id int64, coverURL string) (CollectionDefinition, error) {
	if !Can(actor.Role, PermissionProductsEdit) { return CollectionDefinition{}, ErrForbidden }
	coverURL = strings.TrimSpace(coverURL)
	if coverURL == "" { return CollectionDefinition{}, fmt.Errorf("%w: пустой адрес обложки", ErrInvalidInput) }
	tag, err := repository.pool.Exec(ctx, `UPDATE collections SET cover_url=$2,updated_at=CURRENT_TIMESTAMP WHERE id=$1`, id, coverURL)
	if err != nil { return CollectionDefinition{}, fmt.Errorf("update collection cover: %w", err) }
	if tag.RowsAffected() != 1 { return CollectionDefinition{}, pgx.ErrNoRows }
	items, err := repository.ListCollectionDefinitions(ctx)
	if err != nil { return CollectionDefinition{}, err }
	for _, item := range items { if item.ID == id { return item, nil } }
	return CollectionDefinition{}, pgx.ErrNoRows
}
