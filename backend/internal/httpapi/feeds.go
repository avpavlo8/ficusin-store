package httpapi

import (
	"context"
	"encoding/xml"
	"fmt"
	"html"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/avpavlo8/ficusin-store/backend/internal/catalog"
)

type feedCatalog interface {
	ListFeedOffers(context.Context) ([]catalog.FeedOffer, error)
	ListCategories(context.Context) ([]catalog.Category, error)
}

type googleRSS struct {
	XMLName xml.Name      `xml:"rss"`
	Version string        `xml:"version,attr"`
	XMLNSG  string        `xml:"xmlns:g,attr"`
	Channel googleChannel `xml:"channel"`
}
type googleChannel struct {
	Title       string       `xml:"title"`
	Link        string       `xml:"link"`
	Description string       `xml:"description"`
	Items       []googleItem `xml:"item"`
}
type googleItem struct {
	ID               string `xml:"g:id"`
	Title            string `xml:"g:title"`
	Description      string `xml:"g:description"`
	Link             string `xml:"g:link"`
	Image            string `xml:"g:image_link"`
	Availability     string `xml:"g:availability"`
	Price            string `xml:"g:price"`
	Condition        string `xml:"g:condition"`
	ProductType      string `xml:"g:product_type"`
	GroupID          string `xml:"g:item_group_id,omitempty"`
	IdentifierExists string `xml:"g:identifier_exists"`
}

type yandexCatalog struct {
	XMLName xml.Name   `xml:"yml_catalog"`
	Date    string     `xml:"date,attr"`
	Shop    yandexShop `xml:"shop"`
}
type yandexShop struct {
	Name       string           `xml:"name"`
	Company    string           `xml:"company"`
	URL        string           `xml:"url"`
	Currencies []yandexCurrency `xml:"currencies>currency"`
	Categories []yandexCategory `xml:"categories>category"`
	Offers     []yandexOffer    `xml:"offers>offer"`
}
type yandexCurrency struct {
	ID   string `xml:"id,attr"`
	Rate string `xml:"rate,attr"`
}
type yandexCategory struct {
	ID       int64  `xml:"id,attr"`
	ParentID *int64 `xml:"parentId,attr,omitempty"`
	Name     string `xml:",chardata"`
}
type yandexOffer struct {
	ID          string       `xml:"id,attr"`
	Available   bool         `xml:"available,attr"`
	URL         string       `xml:"url"`
	Price       string       `xml:"price"`
	CurrencyID  string       `xml:"currencyId"`
	CategoryID  int64        `xml:"categoryId"`
	Picture     string       `xml:"picture"`
	Name        string       `xml:"name"`
	Description string       `xml:"description"`
	VendorCode  string       `xml:"vendorCode"`
	Param       *yandexParam `xml:"param,omitempty"`
}
type yandexParam struct {
	Name  string `xml:"name,attr"`
	Value string `xml:",chardata"`
}

var markupPattern = regexp.MustCompile(`<[^>]*>`)

func plainFeedText(value string, limit int) string {
	value = html.UnescapeString(markupPattern.ReplaceAllString(value, " "))
	value = strings.Join(strings.Fields(value), " ")
	runes := []rune(value)
	if len(runes) > limit {
		value = string(runes[:limit])
	}
	return value
}

func absoluteFeedURL(base, value string) string {
	value = strings.TrimSpace(value)
	if parsed, err := url.Parse(value); err == nil && parsed.IsAbs() {
		return parsed.String()
	}
	return strings.TrimRight(base, "/") + "/" + strings.TrimLeft(value, "/")
}

func feedTitle(offer catalog.FeedOffer) string {
	title := offer.Name
	if offer.VariantCount > 1 && strings.TrimSpace(offer.Label) != "" {
		title += " — " + strings.TrimSpace(offer.Label)
	}
	return plainFeedText(title, 150)
}

func feedDescription(offer catalog.FeedOffer) string {
	description := plainFeedText(offer.Description, 5000)
	if description == "" {
		description = "Купить " + offer.Name + " в магазине «Фикусин». Доставка по Рязани и России."
	}
	return description
}

func productFeedHandler(logger *slog.Logger, repository feedCatalog) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if repository == nil {
			http.NotFound(response, request)
			return
		}
		if request.URL.Path != "/feeds/google-products.xml" && request.URL.Path != "/feeds/yandex.yml" {
			http.NotFound(response, request)
			return
		}
		offers, err := repository.ListFeedOffers(request.Context())
		if err != nil {
			logger.Error("product feed failed", "error", err)
			http.Error(response, "feed unavailable", http.StatusServiceUnavailable)
			return
		}
		var categories []catalog.Category
		if request.URL.Path == "/feeds/yandex.yml" {
			categories, err = repository.ListCategories(request.Context())
			if err != nil {
				logger.Error("yandex feed categories failed", "error", err)
				http.Error(response, "feed unavailable", http.StatusServiceUnavailable)
				return
			}
		}
		base := siteBase(request)
		response.Header().Set("Content-Type", "application/xml; charset=utf-8")
		response.Header().Set("Cache-Control", "public, max-age=900")
		response.WriteHeader(http.StatusOK)
		_, _ = response.Write([]byte(xml.Header))
		encoder := xml.NewEncoder(response)
		encoder.Indent("", "  ")
		switch request.URL.Path {
		case "/feeds/google-products.xml":
			items := make([]googleItem, 0, len(offers))
			for _, offer := range offers {
				if offer.Stock <= 0 || offer.Price <= 0 || strings.TrimSpace(offer.Image) == "" {
					continue
				}
				item := googleItem{ID: offer.SKU, Title: feedTitle(offer), Description: feedDescription(offer), Link: base + "/product/" + url.PathEscape(offer.ProductCode) + "?sku=" + url.QueryEscape(offer.SKU), Image: absoluteFeedURL(base, offer.Image), Availability: "in_stock", Price: fmt.Sprintf("%.2f RUB", offer.Price), Condition: "new", ProductType: offer.Category, IdentifierExists: "no"}
				if offer.VariantCount > 1 {
					item.GroupID = offer.ProductCode
				}
				items = append(items, item)
			}
			_ = encoder.Encode(googleRSS{Version: "2.0", XMLNSG: "http://base.google.com/ns/1.0", Channel: googleChannel{Title: "Фикусин", Link: base, Description: "Комнатные растения, кашпо и товары для ухода", Items: items}})
		case "/feeds/yandex.yml":
			yandexCategories := make([]yandexCategory, 0, len(categories))
			for _, category := range categories {
				yandexCategories = append(yandexCategories, yandexCategory{ID: category.ID, ParentID: category.ParentID, Name: category.Name})
			}
			yandexOffers := make([]yandexOffer, 0, len(offers))
			for _, offer := range offers {
				if offer.Stock <= 0 || offer.Price <= 0 || offer.CategoryID <= 0 || strings.TrimSpace(offer.Image) == "" {
					continue
				}
				var parameter *yandexParam
				if offer.VariantCount > 1 && strings.TrimSpace(offer.Label) != "" {
					parameter = &yandexParam{Name: "Вариант", Value: plainFeedText(offer.Label, 100)}
				}
				yandexOffers = append(yandexOffers, yandexOffer{ID: offer.SKU, Available: true, URL: base + "/product/" + url.PathEscape(offer.ProductCode) + "?sku=" + url.QueryEscape(offer.SKU), Price: fmt.Sprintf("%.2f", offer.Price), CurrencyID: "RUB", CategoryID: offer.CategoryID, Picture: absoluteFeedURL(base, offer.Image), Name: feedTitle(offer), Description: feedDescription(offer), VendorCode: offer.SKU, Param: parameter})
			}
			_ = encoder.Encode(yandexCatalog{Date: time.Now().Format("2006-01-02 15:04"), Shop: yandexShop{Name: "Фикусин", Company: "Фикусин", URL: base, Currencies: []yandexCurrency{{ID: "RUB", Rate: "1"}}, Categories: yandexCategories, Offers: yandexOffers}})
		}
	})
}
