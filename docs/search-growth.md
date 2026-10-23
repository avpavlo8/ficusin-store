# Search growth foundation

The storefront exposes crawlable category and collection pages instead of
asking search engines to index filter query strings:

- `/catalog/{category-slug}`
- `/collections/{collection-slug}`

Only categories and collections containing published products are added to
`/sitemap.xml`. Private pages remain `noindex` and blocked in `robots.txt`.

## Product feeds

- Google Merchant Center: `/feeds/google-products.xml`
- Yandex Webmaster / product search: `/feeds/yandex.yml`

Both feeds are generated from PostgreSQL and cached publicly for 15 minutes.
They export each in-stock SKU separately, group variants by product code and
link to the exact variant with `?sku=`. Offers without a real image, positive
price or stock are deliberately omitted; publishing invented data is worse
than a smaller feed.

## Ownership verification

The owner can paste the `content` value from the provider's verification meta
tag into store settings:

- `Код подтверждения Яндекс Вебмастера`
- `Код подтверждения Google Search Console`

The value is validated before it reaches HTML. After the provider confirms
ownership, keep the value in settings so verification is not lost later.

## Provider setup after deployment

1. Add `https://ficusin.ru` to Yandex Webmaster and Google Search Console.
2. Select HTML meta-tag verification and save only its `content` value in the
   corresponding store setting.
3. Submit `https://ficusin.ru/sitemap.xml` to both providers.
4. Submit the YML URL in Yandex Webmaster and the Google XML URL in Merchant
   Center.
5. Enable automatic item updates in Merchant Center so the landing page wins
   if price or availability briefly differs while a feed cache expires.

Feeds improve discovery and data accuracy; they do not create demand. Search
growth still requires useful landing copy, links, reviews, content and ongoing
query/conversion analysis.

## Product sections

The product page keeps its tab interface. Once product data has loaded, all
applicable section panels (description, care, reviews and questions) are mounted
in the rendered HTML; inactive panels use the native `hidden` attribute. Clicking
a tab only changes visibility and never fetches its text or scrolls the page.
Keyboard users can switch with Left/Right, Home and End. Incoming care links
continue to open the care tab and scroll to the requested topic.

This removes the click requirement for content discovery in the rendered DOM.
The initial HTTP response also includes a readable product body inside the
React root: description, plant care/passport, warnings, FAQ and public reviews.
This is ordinary visible HTML for every user agent, not a bot-only response or
a `noscript` block. React replaces it with the interactive page on startup, so
the enhanced page has no duplicate fallback text. Product data comes from the
same catalogue lookup that supplies metadata; no extra database call is needed.
This removes the JavaScript requirement for primary catalogue text, while the
interactive tabs keep all panels mounted after product data loads. It does not
guarantee indexing or ranking. The production smoke checks both JavaScript-free
content and normal tab behavior against current public catalogue data.
