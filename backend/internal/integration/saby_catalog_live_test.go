package integration

import (
	"context"
	"github.com/avpavlo8/ficusin-store/backend/internal/catalogsync"
	"os"
	"testing"
	"time"
)

// This manual regression can touch ONLY the synthetic, stockless probe card
// previously authorized by the owner. Ordinary CI always skips it.
func TestSabyCatalogueLiveConnection(t *testing.T) {
	const marker = "FICUSIN-API-TEST-20261031"
	if os.Getenv("SABY_CATALOG_LIVE_CHECK") != marker {
		t.Skip("explicit isolated catalogue probe required")
	}
	if os.Getenv("GITHUB_REF_NAME") != "probe/site-saby-adapter-20261031" {
		t.Fatal("isolated probe branch required")
	}
	c := NewSabyClient(os.Getenv("SABY_APP_CLIENT_ID"), os.Getenv("SABY_APP_SECRET"), os.Getenv("SABY_SECRET_KEY"), 278, 6)
	if !c.Configured() {
		t.Fatal("existing service connection unavailable")
	}
	baseline := catalogsync.Snapshot{VariantID: 3952, SKU: marker, Name: "ТЕСТ API Фикусин — НЕ ПРОДАВАТЬ " + marker, PotCM: 14, HeightCM: 38, PriceMinor: 129990, SabyID: "3952", Code: "8e46fe11-b919-51a8-ad0f-fd2214254782", Article: marker, LookupName: "ТЕСТ API Фикусин"}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	r, e := c.Lookup(ctx, baseline)
	if e != nil || !baseline.Matches(r) {
		t.Fatalf("test baseline changed; no writes: %+v %v", r, e)
	}
	wait := func(waitCtx context.Context, expected catalogsync.Snapshot) bool {
		for range 12 {
			r, e := c.Lookup(waitCtx, expected)
			if e == nil && expected.Matches(r) {
				return true
			}
			select {
			case <-waitCtx.Done():
				return false
			case <-time.After(3 * time.Second):
			}
		}
		return false
	}
	target := baseline
	target.Name += " — проверка сайта"
	target.HeightCM = 39
	target.PriceMinor = 130001
	defer func() {
		restoreCtx, stop := context.WithTimeout(context.Background(), 90*time.Second)
		defer stop()
		if e := c.Write(restoreCtx, baseline, true); e != nil {
			t.Errorf("restore baseline: %v", e)
			return
		}
		if !wait(restoreCtx, baseline) {
			t.Error("baseline restore not verified")
		}
	}()
	if e = c.Write(ctx, target, true); e != nil {
		t.Fatal(e)
	}
	if !wait(ctx, target) {
		t.Fatal("new adapter values not confirmed by live catalogue")
	}
	if e = c.Write(ctx, target, false); e != nil {
		t.Fatal(e)
	}
	if !wait(ctx, target) {
		t.Fatal("same-code replay not confirmed")
	}
	t.Log("Go adapter: existing synthetic ID 3952 updated, price 1300.01 and height 39 confirmed; replay retained the identity. Deferred restoration verifies original 14/38/1299.90.")
}
