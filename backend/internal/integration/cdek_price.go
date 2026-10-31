package integration

// CDEKWithDeclaredValue includes the mandatory 0.75% declared-value charge.
// declaredMinor is the actual value sent as item.cost (discounted unit price
// times quantity), excluding delivery. Round the fee to kopecks, then round
// transport plus fee upward once to a whole ruble. No extra VAT is added.
func CDEKWithDeclaredValue(quotes []CDEKQuote, declaredMinor int64) []CDEKQuote {
	result := append([]CDEKQuote(nil), quotes...)
	feeMinor := (declaredMinor*75 + 5000) / 10000
	for i := range result {
		transport := result[i].transportMinor
		if transport == 0 {
			transport = int64(result[i].Price) * 100
		}
		result[i].transportMinor = transport
		result[i].Price = int((transport + feeMinor + 99) / 100)
	}
	return result
}
