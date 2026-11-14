package avito

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"strconv"
)

// ReadPrice reads seller listings from the same API used by Import. It never
// trusts our optimistic local mirror to confirm an ambiguous price mutation.
func (s *Service) ReadPrice(ctx context.Context, target string) (int64, error) {
	if target == "" || !s.Configured() {
		return 0, errors.New("Нет подключения или объявления Avito")
	}
	token, err := s.tokenFor(ctx)
	if err != nil {
		return 0, err
	}
	for _, status := range []string{"active", "old", "removed", "blocked", "rejected"} {
		for page := 1; page <= 100; page++ {
			endpoint := fmt.Sprintf("%s/core/v1/items?status=%s&page=%d&per_page=100", s.baseURL, url.QueryEscape(status), page)
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
			if err != nil {
				return 0, err
			}
			req.Header.Set("Authorization", "Bearer "+token)
			resp, err := s.client.Do(req)
			if err != nil {
				return 0, err
			}
			if resp.StatusCode/100 != 2 {
				resp.Body.Close()
				return 0, fmt.Errorf("Проверить цену Avito: HTTP %d", resp.StatusCode)
			}
			var body struct {
				Resources []apiItem `json:"resources"`
			}
			err = json.NewDecoder(resp.Body).Decode(&body)
			resp.Body.Close()
			if err != nil {
				return 0, err
			}
			for _, item := range body.Resources {
				if itemID(item.ID) != target {
					continue
				}
				if item.Status == "removed" || item.Status == "blocked" || item.Status == "rejected" {
					return 0, errors.New("Объявление Avito недоступно")
				}
				price, e := strconv.ParseFloat(item.Price.String(), 64)
				if e != nil || math.IsNaN(price) || math.IsInf(price, 0) || price <= 0 {
					return 0, errors.New("Avito не вернул положительную цену")
				}
				return int64(math.Round(price * 100)), nil
			}
			if len(body.Resources) < 100 {
				break
			}
		}
	}
	return 0, errors.New("Объявление Avito не найдено в кабинете продавца")
}
