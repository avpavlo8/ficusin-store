package catalogeditor

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/jackc/pgx/v5"
)

func TestSabyPublicationDecodeKeepsExactNumbersAfterJSONB(t *testing.T) {
	raw := []byte(`{"variantId":1,"attributes":{"large":9007199254740993,"money":0.123456789012345678901}}`)
	if dsn := os.Getenv("TEST_DATABASE_URL"); dsn != "" {
		ctx := context.Background()
		conn, err := pgx.Connect(ctx, dsn)
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Close(ctx)
		if err = conn.QueryRow(ctx, `SELECT $1::jsonb`, raw).Scan(&raw); err != nil {
			t.Fatal(err)
		}
	}
	var request Request
	if err := decodePublicationRequest("saby", raw, &request); err != nil {
		t.Fatal(err)
	}
	for key, expected := range map[string]string{"large": "9007199254740993", "money": "0.123456789012345678901"} {
		number, ok := request.SiteAttributes[key].(json.Number)
		if !ok || string(number) != expected {
			t.Fatalf("%s lost exact native number: %v", key, request.SiteAttributes[key])
		}
	}
	for _, channel := range []string{"wb", "ozon"} {
		if err := decodePublicationRequest(channel, raw, &request); err != nil {
			t.Fatal(err)
		}
		if _, ok := request.SiteAttributes["large"].(float64); !ok {
			t.Fatalf("unrelated %s decoding changed", channel)
		}
	}
	if decodePublicationRequest("saby", []byte(`{} {}`), &request) == nil {
		t.Fatal("trailing data accepted")
	}
}
