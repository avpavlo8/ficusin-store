package catalog

import "time"

// PopularityWeight documents the score applied to one sold unit. Only a
// completed order or a confirmed payment counts; cancelled orders never do.
// ListAvailable applies the same buckets in SQL so sorting happens in one
// catalogue query rather than loading sales history into the application.
func PopularityWeight(age time.Duration, completed, paid, cancelled bool) float64 {
	if cancelled || (!completed && !paid) {
		return 0
	}
	switch {
	case age < 0 || age <= 30*24*time.Hour:
		return 1
	case age <= 90*24*time.Hour:
		return .5
	case age <= 365*24*time.Hour:
		return .2
	default:
		return .05
	}
}
