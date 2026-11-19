package catalogeditor

import "slices"

// SitePhotoBaseline belongs to an individual publication, not the editor draft.
// A queued revision receives its baseline when the preceding revision finishes.
type SitePhotoBaseline struct {
	Before    []string `json:"before"`
	Unmanaged []string `json:"unmanaged,omitempty"`
}

// CarrySitePhotos retains the last confirmed site gallery for the same card.
func CarrySitePhotos(channel, status string, previous, next Request, receipt Progress) *SitePhotoBaseline {
	if next.Action != "update" || previous.ProductID != next.ProductID || previous.VariantID != next.VariantID {
		return nil
	}
	target := receipt.IDs.ProductID
	if channel == "ozon" || channel == "flowwow" {
		target = receipt.IDs.OfferID
	}
	if previous.Action == "update" {
		target = previous.ExistingID
	}
	if target == "" || target != next.ExistingID {
		return nil
	}
	switch status {
	case "linked":
		// Linking an existing card establishes its identity, not ownership
		// of the files already present in its gallery.
		return &SitePhotoBaseline{Before: previous.Images, Unmanaged: previous.Images}
	case "created", "synced", "simulated":
		baseline := &SitePhotoBaseline{Before: previous.Images}
		if receipt.SitePhotos != nil {
			for _, photo := range receipt.SitePhotos.Unmanaged {
				if slices.Contains(previous.Images, photo) {
					baseline.Unmanaged = append(baseline.Unmanaged, photo)
				}
			}
		}
		return baseline
	default:
		return receipt.SitePhotos
	}
}
