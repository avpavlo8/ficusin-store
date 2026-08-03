package catalog

import "testing"

func TestRecommendationScore(t *testing.T) {
    source := ProductDetail{
        CatalogSection: "plants",
        PlantKind:      "aglaonema",
        LightLevel:     "low_light",
        Watering:       "moderate",
        HeightClass:    "medium",
        CareLevel:      "easy",
        Placement:      "office",
        PetSafety:      "caution",
        GrowthHabit:    "compact",
    }

    exact := Product{
        CatalogSection: "plants",
        PlantKind:      "aglaonema",
        LightLevel:     "low_light",
        Watering:       "moderate",
        HeightClass:    "medium",
        CareLevel:      "easy",
        Placement:      "office",
        PetSafety:      "caution",
        GrowthHabit:    "compact",
    }
    sameSection := Product{CatalogSection: "plants"}
    otherSection := Product{CatalogSection: "pots"}

    if recommendationScore(source, exact) <= recommendationScore(source, sameSection) {
        t.Fatal("a product with matching kind and attributes must rank above a section-only match")
    }
    if recommendationScore(source, sameSection) <= recommendationScore(source, otherSection) {
        t.Fatal("a product in the same catalog section must rank above another section")
    }
}

func TestRecommendationScoreIgnoresEmptyAttributes(t *testing.T) {
    source := ProductDetail{CatalogSection: "plants"}
    empty := Product{CatalogSection: "plants"}
    filled := Product{CatalogSection: "plants", LightLevel: "sunny"}

    if recommendationScore(source, empty) != recommendationScore(source, filled) {
        t.Fatal("an empty source attribute must not affect the recommendation score")
    }
}
