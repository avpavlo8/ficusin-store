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
	SortOrder int              `json:"sortOrder"`
	Active    bool             `json:"active"`
	Mode      string           `json:"mode"`
	Rules     []CollectionRule `json:"rules"`
}

type CollectionDefinitionInput struct {
	Slug      string           `json:"slug"`
	Title     string           `json:"title"`
	Note      string           `json:"note"`
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
		SELECT id,slug,title,note,sort_order,is_active<>0,mode,rules
		FROM collections ORDER BY sort_order,id
	`)
	if err != nil { return nil, fmt.Errorf("list collection definitions: %w", err) }
	defer rows.Close()
	items := make([]CollectionDefinition, 0)
	for rows.Next() {
		var item CollectionDefinition
		var raw []byte
		if err := rows.Scan(&item.ID,&item.Slug,&item.Title,&item.Note,&item.SortOrder,&item.Active,&item.Mode,&raw); err != nil { return nil, err }
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
		INSERT INTO collections(slug,title,note,sort_order,is_active,mode,rules,updated_at)
		VALUES($1,$2,$3,$4,$5,$6,$7::jsonb,CURRENT_TIMESTAMP) RETURNING id
	`, input.Slug,input.Title,input.Note,input.SortOrder,boolToSmallInt(input.Active),input.Mode,string(raw)).Scan(&id); err != nil {
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
		UPDATE collections SET slug=$2,title=$3,note=$4,sort_order=$5,is_active=$6,mode=$7,rules=$8::jsonb,updated_at=CURRENT_TIMESTAMP
		WHERE id=$1
	`, id,input.Slug,input.Title,input.Note,input.SortOrder,boolToSmallInt(input.Active),input.Mode,string(raw))
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
