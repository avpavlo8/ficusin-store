package auth

import "errors"

const CookieName = "ficusin_session"

var (
	ErrAccountExists               = errors.New("account already exists")
	ErrAccountNotFound             = errors.New("account not found")
	ErrInvalidCredentials          = errors.New("invalid credentials")
	ErrInvalidCode                 = errors.New("invalid or expired code")
	ErrTooManyAttempts             = errors.New("too many attempts")
	ErrRegistrationDetailsRequired = errors.New("registration details required")
	ErrRequestTooSoon              = errors.New("code was requested too recently")
)

type User struct {
	ID                int64  `json:"id"`
	Email             string `json:"email"`
	Phone             string `json:"phone"`
	FullName          string `json:"fullName"`
	LastName          string `json:"lastName"`
	Patronymic        string `json:"patronymic"`
	DeliveryAddress   string `json:"deliveryAddress"`
	AccountType       string `json:"accountType"`
	WholesaleStatus   string `json:"wholesaleStatus"`
	RetailDiscountBPS int    `json:"retailDiscountBps"`
	AdminRole         string `json:"adminRole,omitempty"`
}

// Registration carries the details needed the first time a phone number
// completes OTP verification. Only FullName, Phone and AccountType are
// required; everything else can be filled in later from the account page.
type Registration struct {
	Flow            string
	FullName        string
	LastName        string
	Patronymic      string
	Phone           string
	Email           string
	DeliveryAddress string
	AccountType     string
	CompanyName     string
	INN             string
	KPP             string
	LegalAddress    string
}
