package integration

import "testing"

func TestCDEKDeclaredValuePrice(t *testing.T) {
	for _, tc := range []struct {
		name                string
		transport, declared int64
		want                int
	}{
		{"owner example", 51500, 435000, 548}, {"round total only", 51510, 10000, 516},
		{"exact ruble", 51500, 40000, 518}, {"half kopeck rounds up", 51500, 200, 516}, {"zero value", 51500, 0, 515},
	} {
		t.Run(tc.name, func(t *testing.T) {
			original := []CDEKQuote{{TariffCode: 136, Price: int((tc.transport + 99) / 100), transportMinor: tc.transport}}
			got := CDEKWithDeclaredValue(original, tc.declared)
			if got[0].Price != tc.want {
				t.Fatalf("got %d want %d", got[0].Price, tc.want)
			}
			if original[0].Price != int((tc.transport+99)/100) {
				t.Fatal("mutated original tariff")
			}
			if CDEKWithDeclaredValue(got, tc.declared)[0].Price != tc.want {
				t.Fatal("added fee twice")
			}
		})
	}
}
