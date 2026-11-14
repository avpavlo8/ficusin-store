package integration

import (
	"encoding/json"
	"fmt"
	"math"
	"net"
	"net/url"
	"strconv"
	"strings"
)

func flowwowPresent(v any) bool {
	if v == nil {
		return false
	}
	if s, ok := v.(string); ok {
		return strings.TrimSpace(s) != ""
	}
	if a, ok := v.([]any); ok {
		return len(a) > 0
	}
	return true
}
func ValidateFlowwowFields(fields []FlowwowField, item map[string]any) error {
	// Round-trip normalizes Go integers and browser JSON values alike.
	raw, _ := json.Marshal(item)
	var values map[string]any
	if json.Unmarshal(raw, &values) != nil {
		return fmt.Errorf("Flowwow: некорректные свойства")
	}
	for _, f := range fields {
		value := values[f.Field]
		present := flowwowPresent(value)
		alternative := false
		for _, dep := range f.DependsOn {
			if flowwowPresent(values[dep]) {
				alternative = true
			}
		}
		if present && alternative {
			return fmt.Errorf("Flowwow: «%s» нельзя передавать вместе с %s", f.Name, strings.Join(f.DependsOn, ", "))
		}
		if !present {
			if f.Required && !alternative {
				return fmt.Errorf("Flowwow: заполните «%s»", f.Name)
			}
			continue
		}
		invalid := func() error { return fmt.Errorf("Flowwow: проверьте значение «%s»", f.Name) }
		number := func(v any) bool {
			n, ok := v.(float64)
			if !ok || math.IsNaN(n) || math.IsInf(n, 0) {
				return false
			}
			if f.Min != nil && n < *f.Min || f.Max != nil && n > *f.Max {
				return false
			}
			if f.Decimals != nil {
				m := math.Pow10(*f.Decimals)
				if math.Abs(n*m-math.Round(n*m)) > 0.000001 {
					return false
				}
			}
			return true
		}
		selected := func(v any) bool {
			for _, x := range f.Values {
				if fmt.Sprint(v) == fmt.Sprint(x.ID) {
					return true
				}
			}
			return false
		}
		unit := func(v any) bool {
			n, ok := v.(float64)
			if !ok {
				return false
			}
			for _, x := range f.Measure {
				if n == float64(x.ID) {
					return true
				}
			}
			return false
		}
		switch f.Type {
		case "text":
			if _, ok := value.(string); !ok {
				return invalid()
			}
		case "bool":
			if _, ok := value.(bool); !ok {
				return invalid()
			}
		case "decimal":
			if f.Field == "price" {
				if text, ok := value.(string); ok {
					value, _ = strconv.ParseFloat(text, 64)
				}
			}
			if !number(value) {
				return invalid()
			}
		case "measure":
			m, ok := value.(map[string]any)
			if !ok || !number(m["val"]) || !unit(m["measure"]) {
				return invalid()
			}
		case "select":
			if !selected(value) {
				return invalid()
			}
		case "multiselect", "multitext", "image", "consist":
			a, ok := value.([]any)
			if !ok || len(a) == 0 {
				return invalid()
			}
			for _, v := range a {
				switch f.Type {
				case "multiselect":
					if !selected(v) {
						return invalid()
					}
				case "multitext", "image":
					s, ok := v.(string)
					if !ok || strings.TrimSpace(s) == "" {
						return invalid()
					}
					if f.Type == "image" && !publicFlowwowImage(s) {
						return invalid()
					}
				case "consist":
					m, ok := v.(map[string]any)
					if !ok || !flowwowPresent(m["name"]) || !unit(m["measure"]) {
						return invalid()
					}
					n, ok := m["quantity"].(float64)
					if !ok || n < 1 || math.Trunc(n) != n {
						return invalid()
					}
				}
			}
		default:
			return fmt.Errorf("Flowwow: неподдерживаемый тип поля «%s»", f.Name)
		}
	}
	return nil
}
func publicFlowwowImage(s string) bool {
	u, e := url.Parse(s)
	if e != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil {
		return false
	}
	host := strings.ToLower(u.Hostname())
	if host == "localhost" || strings.HasSuffix(host, ".local") || strings.HasSuffix(host, ".internal") {
		return false
	}
	if ip := net.ParseIP(host); ip != nil && (ip.IsPrivate() || ip.IsLoopback() || ip.IsUnspecified() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast()) {
		return false
	}
	return true
}
