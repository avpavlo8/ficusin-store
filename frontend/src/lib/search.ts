// Search that forgives.
//
// The old search was `name.includes(query)`, so «фикусс» found nothing and
// «ficus» found nothing either — the customer concluded the shop had no
// ficus at all. This is deliberately simple and runs in the browser over
// the whole catalogue: 245 products is nothing to filter, and a request per
// keystroke would be slower than the filtering it replaces.

export type Searchable = {
  id: string;
  name: string;
  latin: string;
  plantKind?: string;
  catalogSection: string;
};

// The Russian letters that share a keyboard key with a Latin one. Someone
// who forgot to switch layout types «abrec» and means «фикус».
const layout: Record<string, string> = {
  q: "й", w: "ц", e: "у", r: "к", t: "е", y: "н", u: "г", i: "ш", o: "щ",
  p: "з", a: "ф", s: "ы", d: "в", f: "а", g: "п", h: "р", j: "о", k: "л",
  l: "д", z: "я", x: "ч", c: "с", v: "м", b: "и", n: "т", m: "ь",
  "[": "х", "]": "ъ", ";": "ж", "'": "э", ",": "б", ".": "ю",
};

function fromLatinLayout(value: string): string {
  return value.replace(/[a-z[\];',.]/g, (letter) => layout[letter] ?? letter);
}

export function normalise(value: string): string {
  return value.toLowerCase().replace(/ё/g, "е").trim();
}

// Levenshtein distance, capped: past two edits the words are different
// words, and computing the exact number would be wasted work.
function editDistance(left: string, right: string, limit: number): number {
  if (Math.abs(left.length - right.length) > limit) return limit + 1;
  let previous = Array.from({ length: right.length + 1 }, (_, index) => index);
  for (let i = 1; i <= left.length; i += 1) {
    const current = [i];
    let best = i;
    for (let j = 1; j <= right.length; j += 1) {
      const cost = left[i - 1] === right[j - 1] ? 0 : 1;
      current[j] = Math.min(previous[j] + 1, current[j - 1] + 1, previous[j - 1] + cost);
      best = Math.min(best, current[j]);
    }
    if (best > limit) return limit + 1;
    previous = current;
  }
  return previous[right.length];
}

// How close a word has to be to count as a typo. Short words get no slack:
// «дуб» and «зуб» are different plants, but «монстра» and «монстера» are
// the same one typed in a hurry.
function allowedTypos(word: string): number {
  if (word.length <= 4) return 0;
  if (word.length <= 7) return 1;
  return 2;
}

function wordsOf(product: Searchable): string[] {
  return normalise(`${product.name} ${product.latin} ${product.plantKind ?? ""}`)
    .split(/[^a-zа-я0-9]+/)
    .filter(Boolean);
}

// score returns how well a product answers the query, or 0 for no match.
// Higher is better, so the list can be sorted by relevance rather than
// alphabetically — the plant someone typed the name of should be first.
export function score(product: Searchable, query: string): number {
  const terms = normalise(query).split(/\s+/).filter(Boolean);
  if (terms.length === 0) return 0;
  const words = wordsOf(product);
  const haystack = words.join(" ");
  let total = 0;

  for (const rawTerm of terms) {
    // Try the term as typed and as if the layout was wrong; take the better.
    const candidates = [rawTerm, fromLatinLayout(rawTerm)];
    let best = 0;
    for (const term of candidates) {
      if (term.length < 2) continue;
      if (haystack.startsWith(term)) best = Math.max(best, 100);
      for (const word of words) {
        if (word === term) best = Math.max(best, 90);
        else if (word.startsWith(term)) best = Math.max(best, 70);
        else if (word.includes(term)) best = Math.max(best, 40);
        else if (editDistance(word, term, allowedTypos(term)) <= allowedTypos(term)) {
          best = Math.max(best, 25);
        }
      }
    }
    // Every word of the query has to find something. Otherwise «фикус
    // большой» would match every ficus, including the small ones.
    if (best === 0) return 0;
    total += best;
  }
  return total;
}

export function searchProducts<T extends Searchable>(products: T[], query: string): T[] {
  if (!normalise(query)) return products;
  return products
    .map((product) => ({ product, rank: score(product, query) }))
    .filter((entry) => entry.rank > 0)
    .sort((left, right) => right.rank - left.rank)
    .map((entry) => entry.product);
}

// suggestions are the names shown under the search box as you type.
export function suggestions<T extends Searchable>(products: T[], query: string, limit = 6): T[] {
  return searchProducts(products, query).slice(0, limit);
}
