package saby

type CatalogItem struct {
	ID          any      `json:"id"`
	Article     string   `json:"article"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Cost        any      `json:"cost"`
	Balance     any      `json:"balance"`
	Images      []string `json:"images"`
	Published   *bool    `json:"published"`
	IsParent    bool     `json:"isParent"`
}

type Result struct {
	OK        bool   `json:"ok"`
	ItemsRead int    `json:"itemsRead"`
	SyncedAt  string `json:"syncedAt"`
}

type AuthError struct {
	Code string
	Err  error
}

func (err *AuthError) Error() string {
	if err.Err == nil {
		return err.Code
	}
	return err.Err.Error()
}

func (err *AuthError) Unwrap() error {
	return err.Err
}
