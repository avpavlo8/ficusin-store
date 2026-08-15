package saby

import "time"

// CatalogItem — позиция номенклатуры, как её присылает выгрузка из СБИС.
//
// Поля с кодами приняты как any: СБИС отдаёт их то строкой, то числом, и
// строгий тип уронил бы разбор всей выгрузки из-за одной позиции.
type CatalogItem struct {
	ID          any      `json:"id"`
	Article     any      `json:"article"`
	Code        any      `json:"code"`
	ExternalID  any      `json:"externalId"`
	NomNumber   any      `json:"nomNumber"`
	Barcode     any      `json:"barcode"`
	Barcodes    []CatalogBarcode `json:"barcodes"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Cost        any      `json:"cost"`
	Balance     any      `json:"balance"`
	Images      []string `json:"images"`
	Attributes  any      `json:"attributes"`
	Published   *bool    `json:"published"`
	IsParent    bool     `json:"isParent"`
}

// CatalogBarcode — один из штрихкодов позиции. Их несколько: свои с
// этикетки и выданные маркетплейсом.
type CatalogBarcode struct {
	Code     any `json:"code"`
	CodeType any `json:"codeType"`
}

type SalesItem struct {
	Date     string  `json:"date"`
	SabyID   string  `json:"sabyId"`
	Units    int     `json:"units"`
	GrossRUB float64 `json:"grossRub"`
}

type SalesUpload struct {
	From  string      `json:"from"`
	To    string      `json:"to"`
	Items []SalesItem `json:"items"`
}

type SalesResult struct {
	OK                 bool      `json:"ok"`
	Rows               int       `json:"rows"`
	LinkedRows         int       `json:"linkedRows"`
	RecommendationRows int       `json:"recommendationRows"`
	SyncedAt           time.Time `json:"syncedAt"`
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
