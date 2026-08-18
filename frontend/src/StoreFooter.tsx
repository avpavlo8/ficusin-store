import { useEffect, useState } from "react";

type FooterCategory = { id: number; parentId: number | null; name: string; sortOrder: number };

const socialIcons = {
  telegram: "M21.5 4.5 18.7 19c-.2 1-1 1.3-1.8.8l-4.3-3.2-2.1 2c-.2.2-.4.3-.7.2l.4-4.7 8.6-7.8c.4-.4-.1-.6-.6-.3L7.6 12.7l-4.4-1.4c-1-.3-1-1 .2-1.5L20.5 3c.8-.3 1.3.2 1 1.5Z",
  vk: "M3 7.5h3.2c.3 0 .5.2.7.6 1.2 2.8 2.7 4.5 3.5 4.5.3 0 .4-.2.4-.7V8.8c-.1-1.2-.7-1.3-.7-1.7 0-.2.2-.4.5-.4h4.9c.5 0 .7.3.7.8v4.2c0 .4.2.6.4.6.8 0 2.3-1.6 3.2-4.3.2-.5.4-.6.9-.6h3.1c.8 0 1 .4.8 1-.4 1.8-3.1 5-3.1 5.5 0 .4 2.5 2.3 3.5 4.1.4.7 0 1.1-.6 1.1h-3.5c-.6 0-.8-.4-1.6-1.2-1.2-1.3-2.4-2.5-3-2.5-.4 0-.5.2-.5.8v1.9c0 .6-.2.9-1.7.9-2.5 0-5.4-1.5-7.4-4.4C4.3 11.3 2.8 8.9 2.8 8c0-.3.1-.5.2-.5Z",
  instagram: "M8 2h8a6 6 0 0 1 6 6v8a6 6 0 0 1-6 6H8a6 6 0 0 1-6-6V8a6 6 0 0 1 6-6Zm0 2a4 4 0 0 0-4 4v8a4 4 0 0 0 4 4h8a4 4 0 0 0 4-4V8a4 4 0 0 0-4-4H8Zm4 3a5 5 0 1 1 0 10 5 5 0 0 1 0-10Zm0 2a3 3 0 1 0 0 6 3 3 0 0 0 0-6Zm5.5-3.2a1.2 1.2 0 1 1 0 2.4 1.2 1.2 0 0 1 0-2.4Z",
  max: "M5 5.5A4.5 4.5 0 0 1 9.5 1h5A4.5 4.5 0 0 1 19 5.5v8a4.5 4.5 0 0 1-4.5 4.5h-2.2L8 22v-4H7.5A4.5 4.5 0 0 1 3 13.5v-8h2Zm3 3.2v4.6h2v-2.2l2 2.2 2-2.2v2.2h2V8.7h-1.8L12 11l-2.2-2.3H8Z",
};

function SocialIcon({ path }: { path: string }) {
  return <svg viewBox="0 0 24 24" aria-hidden="true"><path d={path} /></svg>;
}

export function StoreFooter() {
  const [categories, setCategories] = useState<FooterCategory[]>([]);
  useEffect(() => {
    fetch("/api/v1/categories", { cache: "no-store" })
      .then((response) => response.json())
      .then((body: { categories?: FooterCategory[] }) => setCategories((body.categories || []).filter((item) => item.parentId == null).sort((a,b) => a.sortOrder-b.sortOrder || a.name.localeCompare(b.name,"ru"))))
      .catch(() => setCategories([]));
  }, []);
  return <footer className="store-footer">
    <div className="store-footer-grid">
      <section className="store-footer-connect"><a className="store-footer-chat" href="https://t.me/ficusin62" target="_blank" rel="noreferrer">Написать в чат <span>◯</span></a><p>Мы в соцсетях</p><div className="store-footer-socials"><a href="https://vk.ru/ficusin" target="_blank" rel="noreferrer" aria-label="Фикусин во ВКонтакте"><SocialIcon path={socialIcons.vk}/></a><a href="https://t.me/ficusin62" target="_blank" rel="noreferrer" aria-label="Фикусин в Telegram"><SocialIcon path={socialIcons.telegram}/></a><a href="https://www.instagram.com/ficusin_62/" target="_blank" rel="noreferrer" aria-label="Фикусин в Instagram"><SocialIcon path={socialIcons.instagram}/></a><a href="https://max.ru/channel_ficusin" target="_blank" rel="noreferrer" aria-label="Фикусин в MAX"><SocialIcon path={socialIcons.max}/></a></div></section>
      <div className="store-footer-menu"><details open><summary>Каталог</summary><nav>{categories.map((item)=><a key={item.id} href={`/?category=${item.id}#catalog`}>{item.name}</a>)}</nav></details><details open><summary>Покупателям</summary><nav><a href="/delivery-and-returns">Доставка, оплата и возврат</a><a href="/favorites">Избранное</a><a href="/account">Личный кабинет</a></nav></details><details open><summary>Информация</summary><nav><a href="/#about">О компании</a><a href="/contacts">Контакты</a><a href="/offer">Публичная оферта</a><a href="/privacy">Политика конфиденциальности</a><a href="/requisites">Реквизиты</a></nav></details></div>
      <section className="store-footer-contact"><a href="tel:+79156151100">+7 915 615-11-00</a><span>Ежедневно, 08:00–20:00</span><a href="mailto:info@ficusin.ru">info@ficusin.ru</a><span>Рязань, Новосёлов, 40А</span></section>
      <section className="store-footer-brand"><a href="/">Фикусин</a><p>Растения, с которыми хорошо</p></section>
    </div>
    <div className="store-footer-bottom"><span>© Фикусин, {new Date().getFullYear()}</span><a href="/privacy">Политика конфиденциальности</a><a href="/offer">Публичная оферта</a><span>Растения, с которыми хорошо</span></div>
  </footer>;
}
