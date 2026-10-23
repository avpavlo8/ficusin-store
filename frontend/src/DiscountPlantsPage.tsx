import { useCallback, useEffect, useState } from "react";
import { StoreHeader } from "./StoreHeader";
import { sharedAPIJSON } from "./lib/api";
import { usePageTitle } from "./lib/usePageTitle";
import "./styles/discount-plants.css";
import "./styles/customer-pages.css";

type Plant = {
  description?: string;
  id: number;
  article: string;
  name: string;
  label: string;
  price: number;
  discountedPrice: number;
  discountPercent: number;
  image: string;
};
const money = (price: number) => price.toLocaleString("ru-RU", { maximumFractionDigits: 2 }) + " ₽";

export default function DiscountPlantsPage() {
  usePageTitle("Растения со скидкой");
  const [items, setItems] = useState<Plant[]>([]);
  const [status, setStatus] = useState<"loading" | "ready" | "error">("loading");
  const [failedImages, setFailedImages] = useState<Set<number>>(new Set());
  const load = useCallback((force = false) => {
    void sharedAPIJSON<{ items?: Plant[] }>("/api/v1/discount-plants", force)
      .then((data) => {
        setItems(data.items || []);
        setStatus("ready");
      })
      .catch(() => setStatus("error"));
  }, []);
  useEffect(() => {
    load();
  }, [load]);
  return (
    <>
      <StoreHeader />
      <main className="discount-plants">
        <nav className="customer-breadcrumbs" aria-label="Хлебные крошки">
          <a href="/">Главная</a>
          <span aria-hidden="true">/</span>
          <span aria-current="page">Растения со скидкой</span>
        </nav>
        <header>
          <h1>Растения со скидкой</h1>
          <p>
            Эти растения вернулись к нам после поездки и сейчас восстанавливаются. У каждого — своя
            история и цена с учётом состояния. Скидка относится к конкретному экземпляру.
          </p>
          <p>
            Посмотрите растение в магазине перед покупкой. Назовите менеджеру артикул с карточки —
            он покажет именно этот экземпляр.
          </p>
          <a href="/contacts">Адрес и контакты магазина ↗</a>
        </header>
        {status === "loading" && <p role="status">Загружаем растения…</p>}
        {status === "error" && (
          <div className="discount-state" role="alert">
            <h2>Не удалось загрузить растения</h2>
            <p>Проверьте соединение и попробуйте ещё раз.</p>
            <button
              type="button"
              className="primary-button"
              onClick={() => {
                setStatus("loading");
                load(true);
              }}
            >
              Повторить загрузку
            </button>
          </div>
        )}
        {status === "ready" &&
          (!items.length ? (
            <div className="discount-state">
              <h2>Сейчас таких растений нет</h2>
              <p>
                Все экземпляры со скидкой уже нашли свой дом. Можно выбрать растение в каталоге или
                уточнить наличие в магазине.
              </p>
              <a className="primary-button" href="/#catalog">
                Перейти в каталог
              </a>
              <a href="/contacts">Связаться с магазином</a>
            </div>
          ) : (
            <section className="discount-plants-grid" aria-label="Растения на восстановлении">
              {items.map((item) => (
                <article key={item.id} id={item.article}>
                  <div className="discount-plant-image">
                    {item.image && !failedImages.has(item.id) ? (
                      <img
                        src={item.image}
                        alt={item.name}
                        loading="lazy"
                        width="640"
                        height="640"
                        onError={() => setFailedImages((current) => new Set(current).add(item.id))}
                      />
                    ) : (
                      <p>Фото пока недоступно. Покажем растение в магазине.</p>
                    )}
                    <span>−{item.discountPercent}%</span>
                  </div>
                  <div>
                    <small>На восстановлении · {item.article}</small>
                    {!item.image.startsWith("/api/v1/discount-plants/") && (
                      <p>
                        <small>Фото из каталога. Состояние экземпляра покажем в магазине.</small>
                      </p>
                    )}
                    {item.description && (
                      <p className="discount-plant-description">{item.description}</p>
                    )}
                    <h2>
                      {item.name}
                      {item.label && ` · ${item.label}`}
                    </h2>
                    <p>
                      <strong>{money(item.discountedPrice)}</strong>{" "}
                      <del aria-label={`Прежняя цена ${money(item.price)}`}>
                        {money(item.price)}
                      </del>
                    </p>
                    <a href="/contacts">Посмотреть в магазине ↗</a>
                  </div>
                </article>
              ))}
            </section>
          ))}
      </main>
    </>
  );
}
