// pricing-preview computes local artifacts through the same pure function as
// the site. It has no database, credentials or marketplace clients.
package main

import (
	"encoding/json"
	"github.com/avpavlo8/ficusin-store/backend/internal/procurement"
	"os"
)

func main() {
	var input struct {
		Settings procurement.PricingSettings  `json:"settings"`
		Records  []map[string]json.RawMessage `json:"records"`
	}
	if err := json.NewDecoder(os.Stdin).Decode(&input); err != nil {
		panic(err)
	}
	for _, r := range input.Records {
		var cost float64
		var box *procurement.PricingPackage
		if err := json.Unmarshal(r["cost"], &cost); err != nil {
			panic(err)
		}
		if err := json.Unmarshal(r["package"], &box); err != nil {
			panic(err)
		}
		q := procurement.UnifiedChannelQuotes(input.Settings, cost, box)
		raw, err := json.Marshal(q)
		if err != nil {
			panic(err)
		}
		r["quotes"] = raw
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(input); err != nil {
		panic(err)
	}
}
