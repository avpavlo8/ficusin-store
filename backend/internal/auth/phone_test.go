package auth

import "testing"

func TestNormalizeRussianPhone(t *testing.T) {
	t.Parallel()

	for _, input := range []string{"89156151100", "79156151100", "9156151100", "+7 915 615-11-00"} {
		if actual := NormalizeRussianPhone(input); actual != "+79156151100" {
			t.Fatalf("NormalizeRussianPhone(%q) = %q", input, actual)
		}
	}
	for _, input := range []string{"123", "1111111111", "+1 915 615-11-00"} {
		if actual := NormalizeRussianPhone(input); actual != "" {
			t.Fatalf("NormalizeRussianPhone(%q) = %q, want empty", input, actual)
		}
	}
}
