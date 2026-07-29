package auth

import "errors"

const CookieName = "ficusin_session"

var (
	ErrAccountExists      = errors.New("account already exists")
	ErrInvalidCredentials = errors.New("invalid credentials")
)

type User struct {
	ID                int64  `json:"id"`
	Email             string `json:"email"`
	Phone             string `json:"phone"`
	FullName          string `json:"fullName"`
	AccountType       string `json:"accountType"`
	WholesaleStatus   string `json:"wholesaleStatus"`
	RetailDiscountBPS int    `json:"retailDiscountBps"`
}

type Registration struct {
	FullName     string
	Phone        string
	Email        string
	Password     string
	AccountType  string
	CompanyName  string
	INN          string
	KPP          string
	LegalAddress string
}
