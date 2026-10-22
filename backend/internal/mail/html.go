package mail

import (
	"fmt"
	"html"
	"net/url"
	"regexp"
	"strings"
)

var letterURL = regexp.MustCompile(`https?://[^\s<>]+`)

// HTML presents the durable plain-text message in the shop's visual style.
// Only escaped text and validated links enter the markup. The same renderer
// covers confirmations, tracking, packing, payment links and status letters.
func HTML(letter Letter) string {
	body := strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(letter.Body), signature()))
	var content strings.Builder
	for _, block := range strings.Split(strings.ReplaceAll(body, "\r\n", "\n"), "\n\n") {
		block = strings.TrimSpace(block)
		if block == "" {
			continue
		}
		if strings.HasPrefix(block, "Что в заказе:") {
			content.WriteString(`<h2 style="margin:28px 0 12px;font-size:18px;line-height:26px;color:#353630">Ваши растения</h2><table role="presentation" width="100%" cellspacing="0" cellpadding="0" style="border-collapse:collapse">`)
			for _, line := range strings.Split(strings.TrimPrefix(block, "Что в заказе:"), "\n") {
				line = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "•"))
				if line == "" {
					continue
				}
				parts := strings.SplitN(line, " — ", 2)
				content.WriteString(`<tr><td style="padding:12px 10px 12px 0;border-bottom:1px solid #e7e5db;vertical-align:top">` + html.EscapeString(parts[0]) + `</td><td align="right" style="padding:12px 0;border-bottom:1px solid #e7e5db;white-space:nowrap;vertical-align:top">`)
				if len(parts) == 2 {
					content.WriteString(html.EscapeString(parts[1]))
				}
				content.WriteString(`</td></tr>`)
			}
			content.WriteString(`</table>`)
		} else if strings.HasPrefix(block, "Товары:") {
			content.WriteString(`<table role="presentation" width="100%" cellspacing="0" cellpadding="0" style="margin:20px 0 24px;border-collapse:collapse">`)
			for _, line := range strings.Split(block, "\n") {
				parts := strings.SplitN(line, ": ", 2)
				if len(parts) != 2 {
					continue
				}
				style := `padding:5px 0;color:#626858;`
				if parts[0] == "Итого" {
					style = `padding:12px 0 0;border-top:1px solid #d9ddcf;font-size:20px;font-weight:bold;color:#353630;`
				}
				content.WriteString(`<tr><td style="` + style + `">` + html.EscapeString(parts[0]) + `</td><td align="right" style="` + style + `">` + html.EscapeString(parts[1]) + `</td></tr>`)
			}
			content.WriteString(`</table>`)
		} else {
			style := `margin:20px 0;line-height:26px;`
			if strings.HasPrefix(block, "Здравствуйте") {
				style = `margin:24px 0 16px;font-size:19px;font-weight:bold;line-height:28px;`
			}
			if strings.Contains(block, "Трек-номер СДЭК:") {
				style += `padding:18px;background-color:#edf0e5;border-radius:12px;`
				block = strings.Replace(block, "\nОтследить: ", "\n", 1)
			}
			content.WriteString(`<p style="` + style + `">` + linkedText(block) + `</p>`)
		}
	}
	return fmt.Sprintf(`<!doctype html><html lang="ru"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><meta name="color-scheme" content="light"><title>%s</title></head>
<body style="margin:0;padding:0;background-color:#f4f1e8;color:#353630;font-family:Arial,Helvetica,sans-serif;font-size:16px">
<table role="presentation" width="100%%" cellspacing="0" cellpadding="0" bgcolor="#f4f1e8"><tr><td align="center" style="padding:28px 12px">
<table role="presentation" width="600" cellspacing="0" cellpadding="0" bgcolor="#fffefa" style="width:100%%;max-width:600px;border-radius:20px;overflow:hidden">
<tr><td style="padding:28px 24px 24px;border-bottom:1px solid #e3e5d9"><a href="https://ficusin.ru"><img src="https://ficusin.ru/assets/brand/graphite-v1/wordmark-graphite.png" width="178" height="56" alt="Фикусин" style="display:block;border:0;max-width:100%%;height:auto"></a></td></tr>
<tr><td style="padding:28px 24px 20px"><h1 style="margin:0;color:#353630;font-family:Georgia,'Times New Roman',serif;font-size:30px;font-weight:normal;line-height:38px">%s</h1>%s</td></tr>
<tr><td bgcolor="#e9eddf" style="padding:24px;color:#3e4937;line-height:25px"><strong style="font-size:18px">Мы рядом, если нужна помощь</strong><p style="margin:12px 0"><a href="tel:+79156151100" style="color:#3e4937;text-decoration:underline">+7 915 615-11-00</a><br><a href="mailto:info@ficusin.ru" style="color:#3e4937;text-decoration:underline">info@ficusin.ru</a></p><p style="margin:12px 0">Рязань, ул. Новосёлов, 40А<br>Ежедневно, 08:00–20:00</p><a href="https://ficusin.ru/chat" style="color:#3e4937;text-decoration:underline">Написать в чат</a> &nbsp;·&nbsp; <a href="https://ficusin.ru/contacts" style="color:#3e4937;text-decoration:underline">Как нас найти</a></td></tr>
</table><p style="margin:18px 0 0;font-size:13px;color:#626858"><a href="https://ficusin.ru" style="color:#626858">Фикусин · комнатные растения</a></p></td></tr></table></body></html>`, html.EscapeString(letter.Subject), html.EscapeString(letter.Subject), content.String())
}

func linkedText(text string) string {
	var result strings.Builder
	position := 0
	for _, match := range letterURL.FindAllStringIndex(text, -1) {
		result.WriteString(html.EscapeString(text[position:match[0]]))
		link := text[match[0]:match[1]]
		parsed, err := url.Parse(link)
		if err == nil && parsed.Host != "" && (parsed.Scheme == "https" || parsed.Scheme == "http") {
			label := link
			switch {
			case strings.HasPrefix(link, "https://www.cdek.ru/ru/tracking?"):
				label = "Отследить на сайте СДЭК →"
			case strings.Contains(text[:match[0]], "Оплатить заказ:"):
				label = "Перейти к оплате →"
			}
			result.WriteString(`<a href="` + html.EscapeString(link) + `" style="color:#49623f;text-decoration:underline;overflow-wrap:anywhere;word-break:break-word">` + html.EscapeString(label) + `</a>`)
		} else {
			result.WriteString(html.EscapeString(link))
		}
		position = match[1]
	}
	result.WriteString(html.EscapeString(text[position:]))
	return strings.ReplaceAll(result.String(), "\n", "<br>")
}
