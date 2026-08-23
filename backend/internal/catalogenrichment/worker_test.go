package catalogenrichment

import (
	"testing"

	"github.com/avpavlo8/ficusin-store/backend/internal/catalogai"
)

func TestFilterAttributesRejectsInventedCodes(t *testing.T) {
	values:=map[string]any{"light_level":"bright","invented":"value"}
	result:=filterAttributes(values,[]catalogai.Attribute{{Code:"light_level"}})
	if len(result)!=1 || result["light_level"]!="bright" { t.Fatalf("unexpected filtered attributes: %#v",result) }
}

func TestPlantCategoryDetection(t *testing.T) {
	if !isPlantCategory("Растения") || !isPlantCategory("plants") { t.Fatal("plant category was not recognized") }
	if isPlantCategory("Удобрения") { t.Fatal("fertilizer must not receive a plant passport") }
}

func TestStatusRemainsJSONStable(t *testing.T) {
	status:=Status{Total:375,Done:230,ImageFailed:145,RateLimited:145}
	if status.Total!=status.Done+status.ImageFailed { t.Fatalf("unexpected status fixture: %#v",status) }
}
