package integration

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"math/big"
	"strings"
)

// JSONB changes object order, spacing and numeric notation. Canonicalization
// is exclusively for the receipt checksum: native Data/schema stays untouched.
func canonicalSabyEditorJSON(raw []byte) ([]byte, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return nil, errors.New("invalid Saby receipt JSON boundary")
	}
	value, err := canonicalSabyEditorNumbers(value)
	if err != nil {
		return nil, err
	}
	// encoding/json orders object keys; arrays and scalar types retain meaning.
	return json.Marshal(value)
}

func canonicalSabyEditorNumbers(value any) (any, error) {
	switch node := value.(type) {
	case json.Number:
		return canonicalSabyEditorNumber(node)
	case []any:
		for i, child := range node {
			next, err := canonicalSabyEditorNumbers(child)
			if err != nil {
				return nil, err
			}
			node[i] = next
		}
	case map[string]any:
		for key, child := range node {
			next, err := canonicalSabyEditorNumbers(child)
			if err != nil {
				return nil, err
			}
			node[key] = next
		}
	}
	return value, nil
}

// Exact coefficient/exponent arithmetic avoids both float64 rounding and
// allocating 10^exponent for tiny/large native decimal values.
func canonicalSabyEditorNumber(number json.Number) (json.Number, error) {
	value := string(number)
	sign := ""
	if strings.HasPrefix(value, "-") {
		sign, value = "-", value[1:]
	}
	exponent := new(big.Int)
	if i := strings.IndexAny(value, "eE"); i >= 0 {
		if _, ok := exponent.SetString(value[i+1:], 10); !ok {
			return "", errors.New("invalid Saby receipt exponent")
		}
		value = value[:i]
	}
	fraction := 0
	if i := strings.IndexByte(value, '.'); i >= 0 {
		fraction = len(value) - i - 1
		value = value[:i] + value[i+1:]
	}
	value = strings.TrimLeft(value, "0")
	if value == "" {
		return "0", nil
	}
	digits := strings.TrimRight(value, "0")
	exponent.Add(exponent, big.NewInt(int64(len(value)-len(digits)-fraction)))
	return json.Number(sign + digits + "e" + exponent.String()), nil
}
