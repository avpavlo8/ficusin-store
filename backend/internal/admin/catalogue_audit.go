package admin

import (
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"
)

var plantPotSize = regexp.MustCompile(`(?i)(^|[\s,;/()_-])(?:d|ø)\s*\.?\s*[0-9]+(?:[.,][0-9]+)?(?:\s*(?:см|мм))?($|[\s,;/()_-])`)
var plantPotDiameter = regexp.MustCompile(`(?i)(?:^|[\s,;/()_-])(?:d|ø)\s*\.?\s*([0-9]{1,3})(?:$|[\s,;/()_-])`)

type CatalogueAudit struct {
	TotalProducts   int `json:"totalProducts"`
	Drafts          int `json:"drafts"`
	PositiveDrafts  int `json:"positiveDrafts"`
	SuggestedGroups int `json:"suggestedGroups"`
	CardsInGroups   int `json:"cardsInGroups"`
	UnmatchedDrafts int `json:"unmatchedDrafts"`
}

type MergeSuggestion struct {
	Key        string    `json:"key"`
	Title      string    `json:"title"`
	Reason     string    `json:"reason"`
	Confidence string    `json:"confidence"`
	Products   []Product `json:"products"`
}

type MergeSuggestionResult struct {
	Audit       CatalogueAudit    `json:"audit"`
	Suggestions []MergeSuggestion `json:"suggestions"`
}

// BuildMergeSuggestions deliberately returns only exact matches after removing
// an explicit pot diameter (D12/D17). Fuzzy matching is too dangerous here:
// Aglaonema Maria and Aglaonema Maria Christina are different cultivars.
func BuildMergeSuggestions(products []Product) MergeSuggestionResult {
	result := MergeSuggestionResult{Suggestions: []MergeSuggestion{}}
	groups := map[string][]Product{}
	for _, product := range products {
		result.Audit.TotalProducts++
		if product.Status != "draft" {
			continue
		}
		result.Audit.Drafts++
		if product.Stock > 0 {
			result.Audit.PositiveDrafts++
		}
		if product.CatalogSection != "plants" {
			continue
		}
		key := mergePlantKey(product.Name)
		if key == "" {
			continue
		}
		category := int64(0)
		if product.CategoryID != nil {
			category = *product.CategoryID
		}
		groups[key+"|"+strconv.FormatInt(category, 10)] = append(groups[key+"|"+strconv.FormatInt(category, 10)], product)
	}
	for _, group := range groups {
		if len(group) < 2 {
			continue
		}
		sort.SliceStable(group, func(i, j int) bool {
			left, right := contentScore(group[i]), contentScore(group[j])
			if left != right {
				return left > right
			}
			return group[i].ID < group[j].ID
		})
		result.Suggestions = append(result.Suggestions, MergeSuggestion{
			Key: mergePlantKey(group[0].Name), Title: mergePlantKey(group[0].Name),
			Reason: "Совпадает название после удаления только диаметра горшка",
			Confidence: "high", Products: group,
		})
		result.Audit.CardsInGroups += len(group)
	}
	sort.Slice(result.Suggestions, func(i, j int) bool { return result.Suggestions[i].Title < result.Suggestions[j].Title })
	result.Audit.SuggestedGroups = len(result.Suggestions)
	result.Audit.UnmatchedDrafts = result.Audit.Drafts - result.Audit.CardsInGroups
	return result
}

func mergePlantKey(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	for plantPotSize.MatchString(name) {
		name = plantPotSize.ReplaceAllString(name, "$1 $2")
	}
	name = strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return r
		}
		return ' '
	}, name)
	return strings.Join(strings.Fields(name), " ")
}

func contentScore(product Product) int {
	score := 0
	for _, value := range []string{product.LatinName, product.ShortDescription, product.Description, product.CareInstructions, product.Image} {
		if strings.TrimSpace(value) != "" {
			score++
		}
	}
	if product.CategoryID != nil {
		score++
	}
	return score
}

func potDiameterFromName(name string) (int, bool) {
	match := plantPotDiameter.FindStringSubmatch(name)
	if len(match) != 2 {
		return 0, false
	}
	diameter, err := strconv.Atoi(match[1])
	if err != nil || diameter < 2 || diameter > 100 {
		return 0, false
	}
	return diameter, true
}
