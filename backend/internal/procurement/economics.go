package procurement

import (
	"fmt"
	"math"
)

// Missing charges are distinct from an explicitly verified zero. All amounts
// are seller charges per completed sale, at the seller's price before platform
// funded buyer discounts. Logistics includes last mile and processing; losses
// include the expected unrecovered cost of cancellations, returns and writeoffs.
type ChannelCosts struct {
	CommissionRate *float64 `json:"commissionRate"`
	AcquiringRate  *float64 `json:"acquiringRate"`
	OtherRate      *float64 `json:"otherRate,omitempty"`
	PackagingRUB   *float64 `json:"packagingRub"`
	LogisticsRUB   *float64 `json:"logisticsRub"`
	LossRUB        *float64 `json:"lossRub"`
}

type PricingEconomics struct {
	MonthlyFixedCostsRUB float64                 `json:"monthlyFixedCostsRub"`
	Channels             map[string]ChannelCosts `json:"channels"`
	Unified              *UnifiedPricing         `json:"unified,omitempty"`
}

type ChannelQuote struct {
	Model           string          `json:"model,omitempty"`
	TargetRUB       float64         `json:"targetRub,omitempty"`
	Costs           *ChannelCosts   `json:"costs,omitempty"`
	Package         *PricingPackage `json:"package,omitempty"`
	Notes           []string        `json:"notes,omitempty"`
	Manual          bool            `json:"manual"`
	PriceRUB        *int64          `json:"priceRub,omitempty"`
	BreakEvenRUB    *int64          `json:"breakEvenRub,omitempty"`
	ContributionRUB *float64        `json:"contributionRub,omitempty"`
	CoverUnits      *int64          `json:"coverUnits,omitempty"`
	Missing         []string        `json:"missing"`
}

func finiteNonnegative(v float64) bool { return v >= 0 && !math.IsNaN(v) && !math.IsInf(v, 0) }

func validateChannelCosts(costs ChannelCosts, tax float64) bool {
	for _, v := range []*float64{costs.CommissionRate, costs.AcquiringRate, costs.OtherRate, costs.PackagingRUB, costs.LogisticsRUB, costs.LossRUB} {
		if v != nil && !finiteNonnegative(*v) {
			return false
		}
	}
	if !finiteNonnegative(tax) || tax >= 1 {
		return false
	}
	rate := tax
	for _, v := range []*float64{costs.CommissionRate, costs.AcquiringRate, costs.OtherRate} {
		if v != nil {
			rate += *v
		}
	}
	return rate < 1
}

func (costs ChannelCosts) missing() []string {
	missing := []string{}
	for _, v := range []struct {
		name  string
		value *float64
	}{
		{"комиссия", costs.CommissionRate}, {"эквайринг", costs.AcquiringRate},
		{"упаковка", costs.PackagingRUB}, {"логистика", costs.LogisticsRUB}, {"потери и возвраты", costs.LossRUB},
	} {
		if v.value == nil {
			missing = append(missing, v.name)
		}
	}
	return missing
}

func channelQuote(cost, contribution, tax float64, costs ChannelCosts, fixed float64) ChannelQuote {
	quote := ChannelQuote{Missing: costs.missing()}
	if len(quote.Missing) != 0 {
		return quote
	}
	if !validateChannelCosts(costs, tax) || !finiteNonnegative(cost) || !finiteNonnegative(contribution) {
		quote.Missing = []string{"проверьте расходы: сумма удержаний должна быть меньше 100%"}
		return quote
	}
	keep := retainedRate(tax, costs)
	variable := cost + *costs.PackagingRUB + *costs.LogisticsRUB + *costs.LossRUB
	// Round upwards: rounding a floor down would create a loss at the floor.
	floor := math.Ceil(variable / keep)
	price := math.Ceil((variable + contribution) / keep)
	if math.IsInf(price, 0) || price > 1e12 {
		quote.Missing = []string{"проверьте сумму расходов"}
		return quote
	}
	p, f := int64(price), int64(floor)
	actual := price*keep - variable
	quote.PriceRUB, quote.BreakEvenRUB, quote.ContributionRUB = &p, &f, &actual
	if actual > 0 && fixed > 0 && fixed/actual < 1e12 {
		n := int64(math.Ceil(fixed / actual))
		quote.CoverUnits = &n
	}
	return quote
}

// Retail remains a candidate for comparison with the market, not a claim that
// this markup guarantees profit. Each marketplace must retain the same ruble
// contribution as a direct sale after its own fees, unless a market price is explicitly chosen. Overheads are shown once,
// through the break-even sales volume, rather than a second hidden surcharge.
func calculateChannelQuotes(settings PricingSettings, cost float64, retail int64, overrides map[string]ChannelCosts, marketPrices ...map[string]int64) map[string]ChannelQuote {
	if settings.Economics.Unified != nil && settings.Economics.Unified.Enabled {
		return UnifiedChannelQuotes(settings, cost, nil)
	}
	profiles := make(map[string]ChannelCosts, 3)
	for _, channel := range []string{"site", "wb", "ozon"} {
		profiles[channel] = settings.Economics.Channels[channel]
		if v, ok := overrides[channel]; ok {
			profiles[channel] = v
		}
	}
	site := profiles["site"]
	quotes := map[string]ChannelQuote{}
	siteQuote := channelQuote(cost, 0, settings.TaxRate, site, settings.Economics.MonthlyFixedCostsRUB)
	if siteQuote.PriceRUB == nil {
		quotes["site"] = siteQuote
		for _, channel := range []string{"wb", "ozon"} {
			quotes[channel] = ChannelQuote{Missing: append([]string{"расходы сайта для сравнения дохода"}, profiles[channel].missing()...)}
		}
		return applyMarketPrices(quotes, settings, cost, profiles, marketPrices)
	}
	keep := retainedRate(settings.TaxRate, site)
	variable := cost + *site.PackagingRUB + *site.LogisticsRUB + *site.LossRUB
	contribution := float64(retail)*keep - variable
	if contribution <= 0 {
		contribution = 0
		siteQuote.PriceRUB = nil
		siteQuote.ContributionRUB = nil
		siteQuote.CoverUnits = nil
		siteQuote.Missing = []string{"розничная цена не покрывает себестоимость и расходы продажи"}
	} else {
		siteQuote.PriceRUB = &retail
		siteQuote.ContributionRUB = &contribution
		if settings.Economics.MonthlyFixedCostsRUB > 0 && settings.Economics.MonthlyFixedCostsRUB/contribution < 1e12 {
			n := int64(math.Ceil(settings.Economics.MonthlyFixedCostsRUB / contribution))
			siteQuote.CoverUnits = &n
		}
	}
	quotes["site"] = siteQuote
	for _, channel := range []string{"wb", "ozon"} {
		if siteQuote.PriceRUB == nil {
			quotes[channel] = ChannelQuote{Missing: []string{"прибыльная розничная цена для сравнения дохода"}}
			continue
		}
		quotes[channel] = channelQuote(cost, contribution, settings.TaxRate, profiles[channel], settings.Economics.MonthlyFixedCostsRUB)
	}
	return applyMarketPrices(quotes, settings, cost, profiles, marketPrices)
}

// Explicit market prices may retain a different contribution from the site.
// Profitability and monthly coverage are still checked against their own fees.
func applyMarketPrices(quotes map[string]ChannelQuote, settings PricingSettings, cost float64, profiles map[string]ChannelCosts, inputs []map[string]int64) map[string]ChannelQuote {
	if len(inputs) == 0 {
		return quotes
	}
	for _, channel := range []string{"wb", "ozon"} {
		price := inputs[0][channel]
		if price <= 0 {
			continue
		}
		costs := profiles[channel]
		q := channelQuote(cost, 0, settings.TaxRate, costs, settings.Economics.MonthlyFixedCostsRUB)
		q.Manual = true
		if q.PriceRUB != nil {
			q.CoverUnits = nil
			actual := float64(price)*retainedRate(settings.TaxRate, costs) - cost - *costs.PackagingRUB - *costs.LogisticsRUB - *costs.LossRUB
			if actual <= 0 {
				q.PriceRUB = nil
				q.ContributionRUB = nil
				q.Missing = []string{"выбранная рыночная цена не покрывает товар и расходы продажи"}
			} else {
				p := price
				q.PriceRUB = &p
				q.ContributionRUB = &actual
				if settings.Economics.MonthlyFixedCostsRUB > 0 && settings.Economics.MonthlyFixedCostsRUB/actual < 1e12 {
					n := int64(math.Ceil(settings.Economics.MonthlyFixedCostsRUB / actual))
					q.CoverUnits = &n
				}
			}
		}
		quotes[channel] = q
	}
	return quotes
}

// PDF dimensions are evidence, catalogue names are not. An explicit row review
// may retain a physical measurement; identity matching alone cannot approve it.
func dimensionWarning(pot, height *float64, invoice *AliasReview, reviewed bool, catalogueLabels ...string) string {
	if reviewed || invoice == nil {
		return ""
	}
	differs := func(a, b *float64) bool { return b != nil && *b > 0 && (a == nil || math.Abs(*a-*b) > .05) }
	if differs(pot, invoice.PotDiameterCM) || differs(height, invoice.HeightCM) {
		return "Размеры отличаются от инвойса: проверьте и сохраните фактические D и H"
	}
	if len(catalogueLabels) > 0 && invoice.PotDiameterCM != nil {
		if diameter := parseDimension(catalogueLabels[0], potPattern); diameter != nil && math.Round(*diameter) != math.Round(*invoice.PotDiameterCM) {
			return fmt.Sprintf("Карточка D%g, инвойс D%g: проверьте вариант товара. Не подгоняйте размеры инвойса под карточку.", *diameter, *invoice.PotDiameterCM)
		}
	}
	return ""
}
