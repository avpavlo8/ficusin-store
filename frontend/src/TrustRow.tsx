// Четыре обещания магазина: доставка, упаковка, свежесть, поддержка.
// Показываются там, где покупатель ещё сомневается, — под поиском на
// витрине и рядом с ценой в карточке товара.
const promises: Array<[string, string, string]> = [
  ["\u{1F69A}", "Доставка", "по всей России"],
  ["\u{1F4E6}", "Бережная", "упаковка"],
  ["\u{1F331}", "Свежие", "растения"],
  ["\u{1F4AC}", "Поддержка", "08:00 — 20:00"],
];

export function TrustRow() {
  return (
    <div className="trust-row">
      {promises.map(([icon, title, note]) => (
        <p className="trust-item" key={title + note}>
          <span aria-hidden="true">{icon}</span>
          <span>
            <b>{title}</b>
            <small>{note}</small>
          </span>
        </p>
      ))}
    </div>
  );
}
