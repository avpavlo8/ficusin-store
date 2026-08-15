package migrate

import (
	"os"
	"strings"
	"testing"
)

func TestCatalogHardeningMigrationsPreserveIdentityAndObservability(t *testing.T) {
	checks:=map[string][]string{
		"046_catalog_identity_integrity.sql":{"product_external_ids_product_level_uidx","validate_external_id_variant_owner","MAXVALUE 999999"},
		"047_saby_characteristics.sql":{"ADD COLUMN IF NOT EXISTS characteristics","DEFAULT '{}'::JSONB"},
		"048_media_health_indexes.sql":{"product_media_object_key_idx","media_mirror_card_url_idx"},
	}
	for file,required:=range checks{
		raw,err:=os.ReadFile("../../../timeweb/migrations/"+file);if err!=nil{t.Fatal(err)};sql:=string(raw)
		for _,fragment:=range required{if !strings.Contains(sql,fragment){t.Errorf("%s lost %q",file,fragment)}}
		for _,unsafe:=range []string{"DROP TABLE products","DROP COLUMN saby_id","DROP COLUMN slug"}{if strings.Contains(sql,unsafe){t.Errorf("%s contains compatibility break %s",file,unsafe)}}
	}
}
