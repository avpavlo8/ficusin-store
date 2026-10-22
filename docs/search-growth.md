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
It does not introduce server rendering of the product body or guarantee indexing.
The initial HTTP response still supplies metadata and structured data; full
server rendering remains a separate technical SEO improvement.
