import "./styles/app-discovery.css";
export function GuestLoyaltyNotice() {
  const returnTo = encodeURIComponent(location.pathname + location.search + location.hash);
  return (
    <section className="guest-loyalty-notice" aria-label="Накопительная скидка">
      <strong>Больше растений — больше скидка</strong>
      <p>
        Войдите или создайте аккаунт, чтобы выполненные заказы учитывались в накоплениях и
        увеличивали скидку на следующие покупки.
      </p>
      <p>Каждые 10 000 ₽ выполненных заказов добавляют 1% к персональной скидке — до 10%.</p>
      <div>
        <a href={`/login?returnTo=${returnTo}`}>Войти</a>
        <a href={`/register?returnTo=${returnTo}`}>Создать аккаунт</a>
      </div>
    </section>
  );
}
