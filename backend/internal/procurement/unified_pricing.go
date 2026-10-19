package procurement

import (
	"fmt"
	"math"
)

// Norms price a population of orders, not its single most expensive route.
// A norm's sample size describes the source report, not duplicated SKU rows.
type LogisticsNorm struct {
	MaxLiters float64 `json:"maxLiters"`
	OzonRUB   float64 `json:"ozonRub"`
	Samples   int     `json:"samples"`
	Source    string  `json:"source"`
}
type UnifiedPricing struct {
	Enabled              bool            `json:"enabled"`
	MarkupRate           float64         `json:"markupRate"`
	MaterialsRUB         float64         `json:"materialsRub"`
	SmallBoxRUB          float64         `json:"smallBoxRub"`
	MediumBoxRUB         float64         `json:"mediumBoxRub"`
	LargeBoxRUB          float64         `json:"largeBoxRub"`
	WBFirstLiterRUB      float64         `json:"wbFirstLiterRub"`
	WBAdditionalLiterRUB float64         `json:"wbAdditionalLiterRub"`
	WBHandlingRUB        float64         `json:"wbHandlingRub"`
	VerifiedAt           string          `json:"verifiedAt"`
	Norms                []LogisticsNorm `json:"norms"`
}
type PricingPackage struct {
	LengthCM float64 `json:"lengthCm"`
	WidthCM  float64 `json:"widthCm"`
	HeightCM float64 `json:"heightCm"`
	Source   string  `json:"source"`
	Category string  `json:"category,omitempty"`
}

func (p *PricingPackage) Liters() float64 {
	if p == nil {
		return 0
	}
	return p.LengthCM * p.WidthCM * p.HeightCM / 1000
}
func (p *PricingPackage) valid() bool {
	if p == nil {
		return false
	}
	for _, v := range []float64{p.LengthCM, p.WidthCM, p.HeightCM} {
		if !finiteNonnegative(v) || v <= 0 || v > 1000 {
			return false
		}
	}
	return true
}
func retainedRate(tax float64, c ChannelCosts) float64 {
	other := 0.
	if c.OtherRate != nil {
		other = *c.OtherRate
	}
	return 1 - tax - *c.CommissionRate - *c.AcquiringRate - other
}
func validateUnified(p *UnifiedPricing, channels map[string]ChannelCosts, tax float64) bool {
	if p == nil || !p.Enabled {
		return true
	}
	for _, v := range []float64{p.MarkupRate, p.MaterialsRUB, p.SmallBoxRUB, p.MediumBoxRUB, p.LargeBoxRUB, p.WBFirstLiterRUB, p.WBAdditionalLiterRUB, p.WBHandlingRUB} {
		if !finiteNonnegative(v) || v > 1e6 {
			return false
		}
	}
	if p.MarkupRate <= 0 || p.MarkupRate > 10 || len(p.Norms) == 0 || len(p.Norms) > 100 {
		return false
	}
	last := 0.
	for _, n := range p.Norms {
		if !finiteNonnegative(n.MaxLiters) || n.MaxLiters <= last || n.MaxLiters > 1e6 || !finiteNonnegative(n.OzonRUB) || n.OzonRUB > 1e6 || n.Samples < 0 {
			return false
		}
		last = n.MaxLiters
	}
	for _, name := range []string{"site", "wb", "ozon"} {
		c, ok := channels[name]
		if !ok || len(c.missing()) != 0 || !validateChannelCosts(c, tax) {
			return false
		}
	}
	return true
}

// UnifiedChannelQuotes is shared by invoice calculations, finished-batch
// previews and manual site edits. Manual SKU prices/cost overrides are not
// applied in this mode. Public prices are never changed by this function.
func UnifiedChannelQuotes(s PricingSettings, cost float64, box *PricingPackage) map[string]ChannelQuote {
	quotes := map[string]ChannelQuote{}
	p := s.Economics.Unified
	if p == nil || !p.Enabled {
		return quotes
	}
	if !validateUnified(p, s.Economics.Channels, s.TaxRate) || !finiteNonnegative(cost) || cost <= 0 || cost > 1e9 {
		for _, ch := range []string{"site", "wb", "ozon"} {
			quotes[ch] = ChannelQuote{Model: "unified", Missing: []string{"проверьте себестоимость и настройки общей формулы"}}
		}
		return quotes
	}
	target := cost * p.MarkupRate
	for _, ch := range []string{"site", "wb", "ozon"} {
		costs := s.Economics.Channels[ch]
		notes := []string{}
		if ch != "site" {
			if box != nil && box.Category != "" && box.Category != "plants" {
				quotes[ch] = ChannelQuote{Model: "unified", TargetRUB: target, Missing: []string{"для этой товарной категории нужны отдельные тарифы МП"}}
				continue
			}
			if !box.valid() {
				quotes[ch] = ChannelQuote{Model: "unified", TargetRUB: target, Missing: []string{"нет габаритов упаковки в PIM или связанных карточках МП"}}
				continue
			}
			liters := box.Liters()
			packaging := p.MaterialsRUB + p.LargeBoxRUB
			if liters <= 5 {
				packaging = p.MaterialsRUB + p.SmallBoxRUB
			} else if liters <= 15 {
				packaging = p.MaterialsRUB + p.MediumBoxRUB
			}
			costs.PackagingRUB = &packaging
			if ch == "wb" {
				forward := p.WBFirstLiterRUB + math.Max(0, liters-1)*p.WBAdditionalLiterRUB + p.WBHandlingRUB
				costs.LogisticsRUB = &forward
			} else {
				var selected *LogisticsNorm
				for i := range p.Norms {
					if liters <= p.Norms[i].MaxLiters {
						selected = &p.Norms[i]
						break
					}
				}
				if selected == nil {
					quotes[ch] = ChannelQuote{Model: "unified", TargetRUB: target, Package: box, Missing: []string{"для такого объёма коробки ещё нет норматива Ozon"}}
					continue
				}
				forward := selected.OzonRUB
				costs.LogisticsRUB = &forward
				notes = append(notes, fmt.Sprintf("Норматив Ozon до %g л; %d продаж в исходной группе. %s", selected.MaxLiters, selected.Samples, selected.Source))
			}
			notes = append(notes, "Упаковка и потери — плановые нормативы. Фактическая доставка отдельного заказа может отличаться.")
		}
		q := channelQuote(cost, target, s.TaxRate, costs, s.Economics.MonthlyFixedCostsRUB)
		q.Model = "unified"
		q.TargetRUB = target
		q.Costs = &costs
		q.Notes = notes
		if ch != "site" {
			q.Package = box
		}
		quotes[ch] = q
	}
	return quotes
}
