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
	"/account/reviews":      true,
	"/admin":                true,
	"/cart":                 true,
	"/checkout":             true,
	"/favorites":            true,
	"/offer":                true,
	"/privacy":              true,
	"/requisites":           true,
	"/delivery-and-returns": true,
	"/contacts":             true,
}

func knownAppRoute(path string) bool {
	if appRoutes[path] {
		return true
	}
	// Карточка товара и отдельный заказ — адреса с переменной частью.
	return strings.HasPrefix(path, "/product/") || strings.HasPrefix(path, "/account/orders/") || landingSlug(path, "/catalog/") != "" || landingSlug(path, "/collections/") != ""
}

func landingSlug(path, prefix string) string {
	if !strings.HasPrefix(path, prefix) {
		return ""
	}
	slug := strings.TrimPrefix(path, prefix)
	if slug == "" || strings.Contains(slug, "/") {
		return ""
	}
	if decoded, err := url.PathUnescape(slug); err == nil {
		return decoded
	}
	return ""
}

type sitemapCatalog interface {
	ListAvailable(context.Context) ([]catalog.Product, error)
	ListCategories(context.Context) ([]catalog.Category, error)
}

type sitemapCollections interface {
	ListCollections(context.Context) ([]catalog.Collection, error)
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
func sitemapHandler(logger *slog.Logger, repository sitemapCatalog, collections sitemapCollections) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		base := siteBase(request)
		set := sitemapSet{Xmlns: "http://www.sitemaps.org/schemas/sitemap/0.9"}
		for _, path := range []string{"/", "/delivery-and-returns", "/contacts", "/offer", "/privacy", "/requisites"} {
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
			categories, categoryErr := repository.ListCategories(request.Context())
			if categoryErr != nil {
				logger.Error("sitemap categories failed", "error", categoryErr)
			} else {
				parents := make(map[int64]*int64, len(categories))
				visible := make(map[int64]bool)
				for _, category := range categories {
					parents[category.ID] = category.ParentID
				}
				for _, product := range products {
					current := product.CategoryID
					for current != nil {
						visible[*current] = true
						current = parents[*current]
					}
				}
				for _, category := range categories {
					if visible[category.ID] {
						set.URLs = append(set.URLs, sitemapURL{Location: base + "/catalog/" + url.PathEscape(category.Slug), Changefreq: "daily", Priority: "0.7"})
					}
				}
			}
		}
		if collections != nil {
			items, err := collections.ListCollections(request.Context())
			if err != nil {
				logger.Error("sitemap collections failed", "error", err)
			} else {
				for _, item := range items {
					set.URLs = append(set.URLs, sitemapURL{Location: base + "/collections/" + url.PathEscape(item.Slug), Changefreq: "daily", Priority: "0.7"})
				}
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

type routeMeta struct {
	Title, Description string
	Index              bool
}

var publicRouteMeta = map[string]routeMeta{
	"/":                     {"Фикусин — комнатные растения в Рязани с доставкой по России", "Комнатные растения, кашпо и всё для ухода. Магазин «Фикусин» в Рязани, доставка по России и самовывоз на Новосёлов, 40А.", true},
	"/delivery-and-returns": {"Доставка и оплата комнатных растений — Фикусин", "Самовывоз и курьерская доставка по Рязани, СДЭК и Почта России. Условия оплаты, упаковки и получения живых растений.", true},
	"/contacts":             {"Контакты магазина растений Фикусин в Рязани", "Магазин комнатных растений «Фикусин»: Рязань, улица Новосёлов, 40А. Телефон, Telegram и часы работы.", true},
	"/requisites":           {"Реквизиты магазина Фикусин", "Реквизиты интернет-магазина комнатных растений «Фикусин».", true},
	"/offer":                {"Публичная оферта — Фикусин", "Условия продажи товаров интернет-магазином «Фикусин».", true},
	"/privacy":              {"Политика конфиденциальности — Фикусин", "Политика обработки и защиты персональных данных интернет-магазина «Фикусин».", true},
}

func withRouteMeta(base, path string, shell []byte) []byte {
	if strings.HasPrefix(path, "/product/") || landingSlug(path, "/catalog/") != "" || landingSlug(path, "/collections/") != "" {
		return shell
	}
	meta, public := publicRouteMeta[path]
	if !public {
		meta = routeMeta{Title: "Фикусин", Description: "Интернет-магазин комнатных растений «Фикусин».", Index: false}
	}
	return applyRouteMeta(base, path, shell, meta, nil)
}

func applyRouteMeta(base, path string, shell []byte, meta routeMeta, structured any) []byte {
	page := titlePattern.ReplaceAll(shell, []byte("<title>"+html.EscapeString(meta.Title)+"</title>"))
	page = descriptionPattern.ReplaceAll(page, []byte(`<meta name="description" content="`+html.EscapeString(meta.Description)+`"`))
	head := strings.Builder{}
	if meta.Index {
		head.WriteString(`<link rel="canonical" href="` + html.EscapeString(base+path) + `">` + "\n")
	} else {
		head.WriteString(`<meta name="robots" content="noindex,nofollow">` + "\n")
	}
	head.WriteString(`<meta property="og:title" content="` + html.EscapeString(meta.Title) + `">` + "\n")
	head.WriteString(`<meta property="og:description" content="` + html.EscapeString(meta.Description) + `">` + "\n")
	head.WriteString(`<meta property="og:url" content="` + html.EscapeString(base+path) + `">` + "\n")
	if path == "/" && structured == nil {
		structured = map[string]any{"@context": "https://schema.org", "@graph": []any{
			map[string]any{"@type": "Store", "@id": base + "/#organization", "name": "Фикусин", "url": base, "telephone": "+79156151100", "image": base + "/assets/redesign/home-hero-4k.webp", "address": map[string]any{"@type": "PostalAddress", "streetAddress": "ул. Новосёлов, 40А", "addressLocality": "Рязань", "addressCountry": "RU"}, "openingHoursSpecification": []any{map[string]any{"@type": "OpeningHoursSpecification", "dayOfWeek": []string{"Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday", "Sunday"}, "opens": "08:00", "closes": "20:00"}}, "sameAs": []string{"https://t.me/ficusin62"}},
			map[string]any{"@type": "WebSite", "@id": base + "/#website", "name": "Фикусин", "url": base, "publisher": map[string]any{"@id": base + "/#organization"}, "inLanguage": "ru-RU"},
		}}
	}
	if structured != nil {
		if encoded, err := json.Marshal(structured); err == nil {
			head.WriteString(`<script type="application/ld+json">` + string(encoded) + `</script>` + "\n")
		}
	}
	return bytes.Replace(page, []byte("</head>"), []byte(head.String()+"</head>"), 1)
}

func withLandingMeta(ctx context.Context, logger *slog.Logger, repository sitemapCatalog, collections sitemapCollections, base, path string, shell []byte) ([]byte, bool) {
	var title, description string
	if slug := landingSlug(path, "/catalog/"); slug != "" && repository != nil {
		items, err := repository.ListCategories(ctx)
		if err != nil {
			logger.Error("category landing meta failed", "error", err)
			return shell, false
		}
		for _, item := range items {
			if item.Slug == slug {
				title = item.Name
				description = "Купить " + item.Name + " в магазине «Фикусин»: актуальные цены и наличие, самовывоз в Рязани и доставка по России."
				break
			}
		}
	} else if slug := landingSlug(path, "/collections/"); slug != "" && collections != nil {
		items, err := collections.ListCollections(ctx)
		if err != nil {
			logger.Error("collection landing meta failed", "error", err)
			return shell, false
		}
		for _, item := range items {
			if item.Slug == slug {
				title = item.Title
				description = strings.TrimSpace(item.Note)
				if description == "" {
					description = "Подборка «" + item.Title + "» от магазина растений «Фикусин». Доставка по Рязани и России."
				}
				break
			}
		}
	}
	if title == "" {
		return shell, false
	}
	meta := routeMeta{Title: title + " — купить в Рязани с доставкой | Фикусин", Description: description, Index: true}
	structured := map[string]any{"@context": "https://schema.org", "@graph": []any{
		map[string]any{"@type": "CollectionPage", "@id": base + path + "#page", "name": title, "description": description, "url": base + path, "inLanguage": "ru-RU"},
		map[string]any{"@type": "BreadcrumbList", "itemListElement": []any{
			map[string]any{"@type": "ListItem", "position": 1, "name": "Главная", "item": base + "/"},
			map[string]any{"@type": "ListItem", "position": 2, "name": "Каталог", "item": base + "/#catalog"},
			map[string]any{"@type": "ListItem", "position": 3, "name": title, "item": base + path},
		}},
	}}
	return applyRouteMeta(base, path, shell, meta, structured), true
}

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
var searchVerificationPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{8,160}$`)

// withAnalytics вставляет счётчик Яндекс.Метрики в оболочку страницы.
//
// Номер живёт в настройках магазина, а не в сборке: владелец меняет его в
// панели без выкладки, и Docker-образ не носит в себе чужой аккаунт. Пустое
// или неверное значение означает отсутствие счётчика — лучше остаться без
// цифр, чем повесить сломанный скрипт на каждую страницу магазина.
// analyticsScript — тело загрузчика счётчика, которое отдаётся с нашего же
// адреса по /analytics.js.
//
// Политика безопасности намеренно запрещает встроенные скрипты: script-src
// разрешает только свой origin и mc.yandex.ru. Официальный сниппет Метрики —
// встроенный, и браузер его отклонял. Добавить ради счётчика unsafe-inline
// значило бы открыть дорогу любому внедрённому скрипту, поэтому загрузчик
// приезжает со своего адреса, а он уже подключает tag.js с разрешённого.
func analyticsScript(counter string) string {
	counter = strings.TrimSpace(counter)
	if !metrikaCounterPattern.MatchString(counter) {
		return ""
	}
	return `(function(m,e,t,r,i,k,a){m[i]=m[i]||function(){(m[i].a=m[i].a||[]).push(arguments)};m[i].l=1*new Date();for(var j=0;j<e.scripts.length;j++){if(e.scripts[j].src===r){return}}k=e.createElement(t),a=e.getElementsByTagName(t)[0],k.async=1,k.src=r,a.parentNode.insertBefore(k,a)})(window,document,"script","https://mc.yandex.ru/metrika/tag.js","ym");window.dataLayer=window.dataLayer||[];ym(` + counter + `,"init",{ssr:true,webvisor:true,clickmap:true,trackLinks:true,accurateTrackBounce:true,ecommerce:"dataLayer"});`
}

// withAnalytics подключает счётчик к оболочке страницы.
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
	snippet := `<script src="/analytics.js" async></script>` + "\n" +
		`<noscript><div><img src="https://mc.yandex.ru/watch/` + counter + `" style="position:absolute;left:-9999px" alt=""></div></noscript>` + "\n"
	return bytes.Replace(shell, []byte("</head>"), []byte(snippet+"</head>"), 1)
}

func withSearchVerification(shell []byte, yandex, google string) []byte {
	head := strings.Builder{}
	yandex = strings.TrimSpace(yandex)
	google = strings.TrimSpace(google)
	if searchVerificationPattern.MatchString(yandex) {
		head.WriteString(`<meta name="yandex-verification" content="` + html.EscapeString(yandex) + `">` + "\n")
	}
	if searchVerificationPattern.MatchString(google) {
		head.WriteString(`<meta name="google-site-verification" content="` + html.EscapeString(google) + `">` + "\n")
	}
	if head.Len() == 0 {
		return shell
	}
	return bytes.Replace(shell, []byte("</head>"), []byte(head.String()+"</head>"), 1)
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
		switch detail.CatalogSection {
		case "plants":
			description = detail.Name + ": живое комнатное растение с бережной упаковкой и доставкой по России."
		case "pots":
			description = detail.Name + ": кашпо для дома и офиса. Самовывоз в Рязани и доставка по России."
		case "soil", "fertilizer":
			description = detail.Name + ": товар для ухода за растениями. Самовывоз в Рязани и доставка по России."
		default:
			description = detail.Name + ": купить в магазине «Фикусин» с доставкой по Рязани и России."
		}
	}
	image := ""
	if len(detail.Images) > 0 {
		image = detail.Images[0]
	}

	offers := make([]map[string]any, 0, len(detail.Variants))
	for _, variant := range detail.Variants {
		availability := "https://schema.org/PreOrder"
		if variant.Stock > 0 {
			availability = "https://schema.org/InStock"
		}
		offer := map[string]any{"@type": "Offer", "priceCurrency": "RUB", "availability": availability, "url": base + "/product/" + slug + "?sku=" + url.QueryEscape(variant.SKU), "sku": variant.SKU, "itemCondition": "https://schema.org/NewCondition"}
		if variant.Price > 0 {
			offer["price"] = strconv.FormatFloat(variant.Price, 'f', 2, 64)
		}
		if len(variant.Images) > 0 {
			offer["image"] = variant.Images[0]
		}
		offers = append(offers, offer)
	}
	structured := map[string]any{
		"@context":    "https://schema.org",
		"@type":       "Product",
		"name":        detail.Name,
		"description": description,
		"offers":      offers,
		"brand":       map[string]any{"@type": "Brand", "name": "Фикусин"},
		"productID":   detail.ID,
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
			if strings.TrimSpace(item.Question) != "" && strings.TrimSpace(item.Answer) != "" {
				questions = append(questions, map[string]any{"@type": "Question", "name": item.Question, "acceptedAnswer": map[string]any{"@type": "Answer", "text": item.Answer}})
			}
		}
		if len(questions) > 0 {
			structured["subjectOf"] = map[string]any{"@type": "FAQPage", "mainEntity": questions}
		}
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
