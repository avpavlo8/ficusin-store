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
	ErrEmailTaken                  = errors.New("email already belongs to another account")
	ErrConsentRequired             = errors.New("consent to data processing is required")
)

// ClientMeta describes where a request came from. It is stored alongside
// the consent record so an agreement can be tied to a moment and a device
// later on.
type ClientMeta struct {
	UserAgent string
	IPAddress string
}

// Profile carries the fields a signed-in customer may edit from their
// account page.
type Profile struct {
	FullName        string
	LastName        string
	Patronymic      string
	Email           string
	DeliveryAddress string
}

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
	// AvatarUpdatedAt is empty when no photo was uploaded. The account page
	// uses it both to decide whether to show one and to bust the image
	// cache after a new upload.
	AvatarUpdatedAt string `json:"avatarUpdatedAt,omitempty"`
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
	// Consent is the registration form's agreement to the privacy policy
	// and the offer. Registration is refused without it, and the agreement
	// is written to consent_events together with the new account.
	Consent bool
}
