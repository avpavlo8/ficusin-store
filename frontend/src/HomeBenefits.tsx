import { useEffect, useState } from "react";
import { InstallHint } from "./InstallHint";
import { storeSession } from "./lib/store-session";

export function HomeBenefits() {
  const [signedIn, setSignedIn] = useState<boolean | null>(null);
  useEffect(() => {
    let active = true;
    void storeSession()
      .then((response) => {
        if (active && (response.ok || response.status === 401)) setSignedIn(response.ok);
      })
      .catch(() => undefined);
    return () => {
      active = false;
    };
  }, []);

  return (
    <div className="home-benefits">
      <section className="home-loyalty" aria-labelledby="home-loyalty-title">
        <div>
          <h2 id="home-loyalty-title">Больше растений — больше скидка</h2>
          <p>
            {signedIn
              ? "Копите персональную скидку до 10% на следующие покупки."
              : "Зарегистрируйтесь и копите персональную скидку до 10% на следующие покупки."}
          </p>
          <p>
            Каждые 10 000 ₽ выполненных заказов добавляют 1% к скидке. Оформляйте заказы в своём
            аккаунте — так покупки учитываются в накоплениях.
          </p>
        </div>
        {signedIn !== null && (
          <a className="primary-button" href={signedIn ? "/account" : "/register"}>
            {signedIn ? "Моя скидка" : "Зарегистрироваться"}
          </a>
        )}
        {signedIn === false && <a href="/login">Уже есть аккаунт? Войти</a>}
      </section>
      <InstallHint inline />
    </div>
  );
}
