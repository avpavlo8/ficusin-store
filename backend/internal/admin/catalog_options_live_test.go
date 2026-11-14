package admin

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestAttributeOptionUpdatesPreserveStoredCodesOnLiveDatabase(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	base, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer base.Close()
	schema := fmt.Sprintf("attribute_options_%d", time.Now().UnixNano())
	quoted := pgx.Identifier{schema}.Sanitize()
	if _, err = base.Exec(ctx, "CREATE SCHEMA "+quoted); err != nil {
		t.Fatal(err)
	}
	defer base.Exec(ctx, "DROP SCHEMA "+quoted+" CASCADE")
	config, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	config.ConnConfig.RuntimeParams["search_path"] = schema
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	for _, table := range []string{"attribute_definitions", "attribute_options", "product_attribute_values", "variant_attribute_values", "collections", "admin_audit_log"} {
		id := pgx.Identifier{table}.Sanitize()
		_, err = pool.Exec(ctx, "CREATE TABLE "+id+" (LIKE public."+id+" INCLUDING DEFAULTS INCLUDING GENERATED INCLUDING IDENTITY INCLUDING CONSTRAINTS INCLUDING INDEXES)")
		must(err)
	}
	repository := NewPostgresRepository(pool)
	owner := Actor{CustomerID: 1, Role: RoleOwner}
	input := AttributeDefinitionInput{Code: "quality_light", Name: "Свет", DataType: "enum", Audience: "customer", Scope: "product", Active: true,
		Options: []AttributeOption{{Code: "bright", Label: "Яркий", Active: true}, {Code: "shade", Label: "Тень", Active: true}}}
	definition, err := repository.CreateAttributeDefinition(ctx, owner, input)
	must(err)
	ids := map[string]int64{}
	for _, option := range definition.Options {
		ids[option.Code] = option.ID
	}
	if len(ids) != 2 || ids["bright"] == 0 || ids["shade"] == 0 {
		t.Fatalf("missing initial options: %+v", definition.Options)
	}
	_, err = pool.Exec(ctx, `INSERT INTO product_attribute_values(product_id,attribute_id,value,source) VALUES(1,$1,'"shade"','local')`, definition.ID)
	must(err)
	input.Options = []AttributeOption{{Code: "bright", Label: "Прямой свет", Active: true}, {Code: "diffused", Label: "Рассеянный", Active: true}}
	_, err = repository.UpdateAttributeDefinition(ctx, Actor{CustomerID: 2, Role: RoleManager}, definition.ID, input)
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("manager changed dictionary: %v", err)
	}
	var originalLabel string
	must(pool.QueryRow(ctx, `SELECT label FROM attribute_options WHERE id=$1`, ids["bright"]).Scan(&originalLabel))
	if originalLabel != "Яркий" {
		t.Fatal("forbidden update wrote an option")
	}
	updated, err := repository.UpdateAttributeDefinition(ctx, owner, definition.ID, input)
	must(err)
	options := map[string]AttributeOption{}
	for _, option := range updated.Options {
		options[option.Code] = option
	}
	if len(options) != 3 || options["bright"].ID != ids["bright"] || options["bright"].Label != "Прямой свет" || !options["bright"].Active || options["shade"].ID != ids["shade"] || options["shade"].Active || !options["diffused"].Active {
		t.Fatalf("rename/upsert/removal changed historical option identity: %+v", updated.Options)
	}
	var storedCode, source string
	must(pool.QueryRow(ctx, `SELECT value#>>'{}',source FROM product_attribute_values WHERE product_id=1 AND attribute_id=$1`, definition.ID).Scan(&storedCode, &source))
	if storedCode != "shade" || source != "local" {
		t.Fatalf("existing product value was rewritten: %s/%s", storedCode, source)
	}
	input.Options = append(input.Options, AttributeOption{Code: "shade", Label: "Полутень", Active: true})
	reactivated, err := repository.UpdateAttributeDefinition(ctx, owner, definition.ID, input)
	must(err)
	for _, option := range reactivated.Options {
		if option.Code == "shade" {
			if option.ID != ids["shade"] || !option.Active || option.Label != "Полутень" {
				t.Fatalf("reactivation replaced historical option: %+v", option)
			}
			return
		}
	}
	t.Fatal("reactivated option missing")
}
