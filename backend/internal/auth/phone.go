package auth

import (
	"regexp"
	"strings"
)

var russianPhonePattern = regexp.MustCompile(`^[3-9][0-9]{9}$`)

func NormalizeRussianPhone(value string) string {
	var digits strings.Builder
	for _, character := range value {
		if character >= '0' && character <= '9' {
			digits.WriteRune(character)
		}
	}

	number := digits.String()
	switch {
	case len(number) == 11 && (number[0] == '7' || number[0] == '8'):
		number = number[1:]
	case len(number) == 10:
	default:
		return ""
	}

	if !russianPhonePattern.MatchString(number) || allDigitsEqual(number) {
		return ""
	}
	return "+7" + number
}

func allDigitsEqual(value string) bool {
	for index := 1; index < len(value); index++ {
		if value[index] != value[0] {
			return false
		}
	}
	return true
}
