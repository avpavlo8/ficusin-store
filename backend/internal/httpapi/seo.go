package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"html"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/avpavlo8/ficusin-store/backend/internal/catalog"
)

// Адреса, которые приложение рисует в браузере. Сервер знает о них ровно
// затем, чтобы отвечать «404» на выдуманный адрес и не отвечать им на
// настоящий: раньше любой набор букв возвращал «200 ОК», и поисковики
// заводили в индекс пустые страницы, которых у магазина нет.
var appRoutes = map[string]bool{
	"/":                     true,
	"/login":                true,
	"/register":             true,
	"/account":              true,
	"/account/profile":      true,
	"/account/favorites":    true,
	"/admin":                true,
	"/cart":                 true,
	"/favorites":            true,
	"/offer":                true,
	"/privacy":              true,
	"/requisites":           true,
	"/delivery-and-returns": true,
}

func knownAppRoute(path string) bool {
	if appRoutes[path] {
		return true
	}
	// Карточка товара и отдельный заказ — адреса с переменной частью.
	return strings.HasPrefix(path, "/product/") || strings.HasPrefix(path, "/account/orders/")
}

type sitemapCatalog interface {
	ListAvailable(context.Context) ([]catalog.Product, error)
}

type sitemapURL struct {
	Location   string `xml:"loc"`
	Changefreq string `xml:"changefreq,omitempty"`
	Priority   string `xml:"priority,omitempty"`
}

type sitemapSet struct {
	XMLName xml.Name     `xml:"urlset"`
	Xmlns   string       `xml:"xmlns,attr"`
	URLs    []sitemapURL `xml:"url"`
}

// Карта сайта собирается на лету. Файл, который надо обновлять руками,
// устареет к пятнице: каталог приезжает из Saby каждый день.
//
// Пустой каталог — не повод отдавать ошибку: статические страницы магазина
// существуют независимо от того, ответила ли база.
func sitemapHandler(logger *slog.Logger, repository sitemapCatalog) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		base := siteBase(request)
		set := sitemapSet{Xmlns: "http://www.sitemaps.org/schemas/sitemap/0.9"}
		for _, path := range []string{"/", "/delivery-and-returns", "/offer", "/privacy", "/requisites"} {
			set.URLs = append(set.URLs, sitemapURL{
				Location: base + path, Changefreq: "weekly", Priority: "0.8",
			})
		}
		if repository != nil {
			products, err := repository.ListAvailable(request.Context())
			if err != nil {
				logger.Error("sitemap catalog failed", "error", err)
			}
			for _, product := range products {
				set.URLs = append(set.URLs, sitemapURL{
					Location: base + "/product/" + product.ID, Changefreq: "daily", Priority: "0.6",
				})
			}
		}
		response.Header().Set("Content-Type", "application/xml; charset=utf-8")
		response.Header().Set("Cache-Control", "public, max-age=3600")
		_, _ = response.Write([]byte(xml.Header))
		_ = xml.NewEncoder(response).Encode(set)
	})
}

// За обратным прокси Timeweb схема приходит заголовком, иначе ссылки в карте
// сайта уедут на http и поисковик посчитает их отдельными страницами.
func siteBase(request *http.Request) string {
	scheme := "https"
	if forwarded := request.Header.Get("X-Forwarded-Proto"); forwarded != "" {
		scheme = forwarded
	} else if request.TLS == nil {
		scheme = "http"
	}
	return scheme + "://" + request.Host
}


// ——— Заголовки карточки товара ———
//
// Страница собирается в браузере, поэтому в HTML у всех адресов лежал один
// заголовок на весь магазин: карточка фикуса в выдаче выглядела так же, как
// главная. Поисковику этого хватает, чтобы не показать товар по запросу с его
// названием. Здесь сервер подменяет заголовок, описание и добавляет разметку
// с ценой и наличием — по ней цену показывают прямо в результатах поиска.

type productMetaCatalog interface {
	DetailBySlug(context.Context, string) (catalog.ProductDetail, error)
}

var (
	titlePattern       = regexp.MustCompile(`(?s)<title>.*?</title>`)
	descriptionPattern = regexp.MustCompile(`<meta name="description" content="[^"]*"`)
)

func productSlug(path string) string {
	if !strings.HasPrefix(path, "/product/") {
		return ""
	}
	slug := strings.TrimPrefix(path, "/product/")
	if slug == "" || strings.Contains(slug, "/") {
		return ""
	}
	if decoded, err := url.PathUnescape(slug); err == nil {
		return decoded
	}
	return slug
}

// metrikaCounterPattern не пускает опечатку в страницу: номер счётчика —
// это цифры и ничего кроме, поэтому любое другое значение считается ошибкой
// настройки и не должно доехать до браузера в виде кода.
var metrikaCounterPattern = regexp.MustCompile(`^[0-9]{5,12}$`)

// withAnalytics вставляет счётчик Яндекс.Метрики в оболочку страницы.
//
// Номер живёт в настройках магазина, а не в сборке: владелец меняет его в
// панели без выкладки, и Docker-образ не носит в себе чужой аккаунт. Пустое
// или неверное значение означает отсутствие счётчика — лучше остаться без
// цифр, чем повесить сломанный скрипт на каждую страницу магазина.
func withAnalytics(shell []byte, counter string) []byte {
	counter = strings.TrimSpace(counter)
	if !metrikaCounterPattern.MatchString(counter) {
		return shell
	}
	snippet := `<script>(function(m,e,t,r,i,k,a){m[i]=m[i]||function(){(m[i].a=m[i].a||[]).push(arguments)};m[i].l=1*new Date();for(var j=0;j<e.scripts.length;j++){if(e.scripts[j].src===r){return}}k=e.createElement(t),a=e.getElementsByTagName(t)[0],k.async=1,k.src=r,a.parentNode.insertBefore(k,a)})(window,document,"script","https://mc.yandex.ru/metrika/tag.js","ym");window.dataLayer=window.dataLayer||[];ym(` + counter + `,"init",{ssr:true,webvisor:true,clickmap:true,trackLinks:true,accurateTrackBounce:true,ecommerce:"dataLayer"});</script>` + "\n" +
		`<noscript><div><img src="https://mc.yandex.ru/watch/` + counter + `" style="position:absolute;left:-9999px" alt=""></div></noscript>` + "\n"
	return bytes.Replace(shell, []byte("</head>"), []byte(snippet+"</head>"), 1)
}

func withProductMeta(
	ctx context.Context,
	logger *slog.Logger,
	repository productMetaCatalog,
	base, slug string,
	shell []byte,
) []byte {
	if repository == nil || slug == "" {
		return shell
	}
	detail, err := repository.DetailBySlug(ctx, slug)
	if err != nil {
		// Ненайденный товар — обычное дело: ссылка могла устареть. Страница
		// всё равно откроется, просто с общим заголовком магазина.
		if !errors.Is(err, catalog.ErrNotFound) {
			logger.Error("product meta failed", "slug", slug, "error", err)
		}
		return shell
	}
	if detail.Name == "" {
		return shell
	}

	title := detail.Name + " — купить с доставкой по России | Фикусин"
	description := strings.TrimSpace(detail.ShortDescription)
	if description == "" {
		description = detail.Name + ": комнатное растение с доставкой по всей России. Бережная упаковка, живые растения из питомников."
	}
	image := ""
	if len(detail.Images) > 0 {
		image = detail.Images[0]
	}

	price, available := 0.0, false
	for _, variant := range detail.Variants {
		if price == 0 || variant.Price < price {
			price = variant.Price
		}
		if variant.Stock > 0 {
			available = true
		}
	}
	availability := "https://schema.org/PreOrder"
	if available {
		availability = "https://schema.org/InStock"
	}

	offer := map[string]any{
		"@type":         "Offer",
		"priceCurrency": "RUB",
		"availability":  availability,
		"url":           base + "/product/" + slug,
	}
	if price > 0 {
		offer["price"] = strconv.FormatFloat(price, 'f', 2, 64)
	}
	structured := map[string]any{
		"@context":    "https://schema.org",
		"@type":       "Product",
		"name":        detail.Name,
		"description": description,
		"offers":      offer,
	}
	if detail.Latin != "" {
		structured["alternateName"] = detail.Latin
	}
	if image != "" {
		structured["image"] = image
	}
	if detail.ReviewsCount > 0 {
		structured["aggregateRating"] = map[string]any{"@type": "AggregateRating", "ratingValue": detail.Rating, "reviewCount": detail.ReviewsCount, "bestRating": 5, "worstRating": 1}
	}
	if len(detail.Passport.FAQ) > 0 {
		questions := make([]map[string]any, 0, len(detail.Passport.FAQ))
		for _, item := range detail.Passport.FAQ {
			if strings.TrimSpace(item.Question) != "" && strings.TrimSpace(item.Answer) != "" { questions = append(questions, map[string]any{"@type":"Question", "name":item.Question, "acceptedAnswer":map[string]any{"@type":"Answer", "text":item.Answer}}) }
		}
		if len(questions) > 0 { structured["subjectOf"] = map[string]any{"@type":"FAQPage", "mainEntity":questions} }
	}
	// json.Marshal экранирует «<», поэтому чужое описание не сможет закрыть
	// тег script и подсунуть свой код.
	encoded, err := json.Marshal(structured)
	if err != nil {
		logger.Error("product schema failed", "slug", slug, "error", err)
		encoded = nil
	}

	head := strings.Builder{}
	head.WriteString(`<link rel="canonical" href="` + html.EscapeString(base+"/product/"+slug) + "\">\n")
	head.WriteString(`<meta property="og:type" content="product">` + "\n")
	head.WriteString(`<meta property="og:title" content="` + html.EscapeString(title) + "\">\n")
	head.WriteString(`<meta property="og:description" content="` + html.EscapeString(description) + "\">\n")
	if image != "" {
		head.WriteString(`<meta property="og:image" content="` + html.EscapeString(image) + "\">\n")
	}
	if encoded != nil {
		head.WriteString(`<script type="application/ld+json">` + string(encoded) + "</script>\n")
	}

	page := titlePattern.ReplaceAll(shell, []byte("<title>"+html.EscapeString(title)+"</title>"))
	page = descriptionPattern.ReplaceAll(
		page, []byte(`<meta name="description" content="`+html.EscapeString(description)+"\""),
	)
	return bytes.Replace(page, []byte("</head>"), []byte(head.String()+"</head>"), 1)
}
