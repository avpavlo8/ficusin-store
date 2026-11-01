// Package catalogsync owns explicit site-to-Saby catalogue commands. Catalogue
// imports and stock sync never enqueue these commands.
package catalogsync

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"strings"
	"time"
)

var ErrConflict = errors.New("данные изменились")
var ErrInvalid = errors.New("неполные данные")
var ErrNotFound = errors.New("размер не найден")

// Snapshot is immutable once queued. Money is integer kopecks throughout.
type Snapshot struct {
	VariantID  int64   `json:"variantId"`
	ProductID  int64   `json:"productId"`
	SKU        string  `json:"sku"`
	Name       string  `json:"name"`
	PotCM      float64 `json:"potCm"`
	HeightCM   float64 `json:"heightCm"`
	PriceMinor int64   `json:"priceMinor"`
	SabyID     string  `json:"sabyId"`
	Code       string  `json:"code"`
	Article    string  `json:"article"`
	LookupName string  `json:"lookupName"`
}

func (s Snapshot) Hash() string {
	b, _ := json.Marshal(struct {
		VariantID       int64
		SKU, Name, Code string
		Pot, Height     float64
		Price           int64
	}{s.VariantID, s.SKU, s.Name, s.Code, s.PotCM, s.HeightCM, s.PriceMinor})
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}
func (s Snapshot) Validate() error {
	if strings.TrimSpace(s.Name) == "" || len([]rune(s.Name)) > 500 || s.SKU == "" || math.IsNaN(s.PotCM) || math.IsInf(s.PotCM, 0) || math.IsNaN(s.HeightCM) || math.IsInf(s.HeightCM, 0) || s.PotCM <= 0 || s.PotCM > 500 || s.HeightCM <= 0 || s.HeightCM > 3000 || s.PriceMinor <= 0 || s.PriceMinor > 1_000_000_000 || s.Code == "" {
		return fmt.Errorf("%w: укажите название, горшок, высоту и положительную цену; для связанного товара нужен код СБИС", ErrInvalid)
	}
	return nil
}

// Remote uses catalogue identity, not the search position or displayed name.
type Remote struct {
	ID, Code, Article, Name string
	PotCM, HeightCM         float64
	PriceMinor              int64
}

func (s Snapshot) Matches(r *Remote) bool {
	return r != nil && r.Code == s.Code && (s.SabyID == "" || s.SabyID == r.ID) && r.Name == s.Name && r.PotCM == s.PotCM && r.HeightCM == s.HeightCM && r.PriceMinor == s.PriceMinor
}

type Command struct {
	ClaimToken  string
	Snapshot    Snapshot
	Revision    int64
	State       string
	AttemptedAt *time.Time
}
type View struct {
	Configured  bool       `json:"configured"`
	Simulation  bool       `json:"simulation,omitempty"`
	State       string     `json:"state"`
	Message     string     `json:"message"`
	Snapshot    Snapshot   `json:"snapshot"`
	Fingerprint string     `json:"fingerprint"`
	Dirty       bool       `json:"dirty"`
	Validation  string     `json:"validation"`
	VerifiedAt  *time.Time `json:"verifiedAt,omitempty"`
}
type Provider interface {
	Configured() bool
	Lookup(context.Context, Snapshot) (*Remote, error)
	Write(context.Context, Snapshot, bool) error
}

// A rejected JSON-RPC request has a definite negative result. All transport
// failures (including a malformed 2xx response) remain unknown.
type RejectedError struct{ Message string }

func (e *RejectedError) Error() string { return e.Message }

type Store interface {
	View(context.Context, int64) (View, error)
	Enqueue(context.Context, int64, int64, string, bool) (View, error)
	Claim(context.Context) (*Command, error)
	Sent(context.Context, *Command) error
	Finish(context.Context, *Command, *Remote) error
	Release(context.Context, *Command, string, string, time.Duration) error
}
type Service struct {
	store    Store
	provider Provider
	wake     chan struct{}
	logger   *slog.Logger
}

func New(store Store, provider Provider, logger *slog.Logger) *Service {
	return &Service{store: store, provider: provider, wake: make(chan struct{}, 1), logger: logger}
}
func (s *Service) decorate(v *View) {
	v.Configured = s.provider != nil && s.provider.Configured()
	if provider, ok := s.provider.(interface{ IsSimulation() bool }); ok && provider.IsSimulation() {
		v.Simulation = true
		if v.State == "synced" {
			v.Message = "Тестовый прогон подтверждён. Рабочий СБИС не изменялся"
		}
	}
}
func (s *Service) View(ctx context.Context, id int64) (View, error) {
	v, e := s.store.View(ctx, id)
	s.decorate(&v)
	return v, e
}
func (s *Service) Enqueue(ctx context.Context, id, actor int64, hash string, check bool) (View, error) {
	if s.provider == nil || !s.provider.Configured() {
		return View{}, fmt.Errorf("%w: подключение СБИС не настроено", ErrInvalid)
	}
	v, e := s.store.Enqueue(ctx, id, actor, hash, check)
	s.decorate(&v)
	if e == nil {
		select {
		case s.wake <- struct{}{}:
		default:
		}
	}
	return v, e
}
func (s *Service) Run(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		case <-s.wake:
		}
		for range 8 {
			if !s.step(ctx) {
				break
			}
		}
	}
}
func (s *Service) step(ctx context.Context) bool {
	if s.provider == nil || !s.provider.Configured() {
		return false
	}
	c, e := s.store.Claim(ctx)
	if e != nil {
		if !errors.Is(e, context.Canceled) {
			s.logger.Error("claim catalogue export", "error", e)
		}
		return false
	}
	if c == nil {
		return false
	}
	taskCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	release := func(state, message string, delay time.Duration) {
		if e := s.store.Release(ctx, c, state, message, delay); e != nil {
			s.logger.Error("save catalogue export result", "variant", c.Snapshot.VariantID, "error", e)
		}
	}
	r, e := s.provider.Lookup(taskCtx, c.Snapshot)
	if e != nil {
		if c.AttemptedAt == nil {
			release("error", e.Error(), 0)
		} else {
			release("unknown", "СБИС пока не подтвердил результат: "+e.Error(), time.Minute)
		}
		return true
	}
	if r != nil && (r.ID == "" || r.Code != c.Snapshot.Code || (c.Snapshot.SabyID != "" && r.ID != c.Snapshot.SabyID)) {
		state := "error"
		if c.AttemptedAt != nil {
			state = "unknown"
		}
		release(state, "Идентификаторы СБИС не совпадают. Проверьте связь товара.", time.Minute)
		return true
	}
	if c.Snapshot.Matches(r) {
		if e := s.store.Finish(ctx, c, r); e != nil {
			release("unknown", "Данные в СБИС найдены, но связь не сохранена. Нужна повторная проверка.", time.Minute)
			s.logger.Error("link exported Saby catalogue item", "error", e)
		}
		return true
	}
	if c.AttemptedAt != nil {
		state := "verifying"
		delay := 10 * time.Second
		if time.Since(*c.AttemptedAt) > 15*time.Minute {
			state = "unknown"
			delay = 5 * time.Minute
		}
		release(state, "Отправлено. Ожидаем подтверждения горшка, высоты и цены от СБИС.", delay)
		return true
	}
	if c.Snapshot.SabyID != "" && r == nil {
		release("error", "Связанная карточка СБИС не найдена. Проверьте связь; новая карточка не создавалась.", 0)
		return true
	}
	// Revalidate the site snapshot and mark the irreversible boundary durably
	// BEFORE POST. A process restart after this point can only perform GET.
	if e := s.store.Sent(ctx, c); e != nil {
		release("error", "Данные на сайте изменились. Сохраните карточку и отправьте заново.", 0)
		return true
	}
	includePrice := r == nil || r.PriceMinor != c.Snapshot.PriceMinor
	if e := s.provider.Write(taskCtx, c.Snapshot, includePrice); e != nil {
		var rejected *RejectedError
		if errors.As(e, &rejected) {
			release("error", e.Error(), 0)
		} else {
			release("unknown", "Ответ СБИС не получен. Проверяем результат без повторного создания.", 10*time.Second)
		}
		return true
	}
	release("verifying", "Отправлено. Проверяем результат в СБИС.", 5*time.Second)
	return true
}
