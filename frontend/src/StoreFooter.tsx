const socialIcons = {
  telegram: "M21.5 4.5 18.7 19c-.2 1-1 1.3-1.8.8l-4.3-3.2-2.1 2c-.2.2-.4.3-.7.2l.4-4.7 8.6-7.8c.4-.4-.1-.6-.6-.3L7.6 12.7l-4.4-1.4c-1-.3-1-1 .2-1.5L20.5 3c.8-.3 1.3.2 1 1.5Z",
  vk: "M3 7.5h3.2c.3 0 .5.2.7.6 1.2 2.8 2.7 4.5 3.5 4.5.3 0 .4-.2.4-.7V8.8c-.1-1.2-.7-1.3-.7-1.7 0-.2.2-.4.5-.4h4.9c.5 0 .7.3.7.8v4.2c0 .4.2.6.4.6.8 0 2.3-1.6 3.2-4.3.2-.5.4-.6.9-.6h3.1c.8 0 1 .4.8 1-.4 1.8-3.1 5-3.1 5.5 0 .4 2.5 2.3 3.5 4.1.4.7 0 1.1-.6 1.1h-3.5c-.6 0-.8-.4-1.6-1.2-1.2-1.3-2.4-2.5-3-2.5-.4 0-.5.2-.5.8v1.9c0 .6-.2.9-1.7.9-2.5 0-5.4-1.5-7.4-4.4C4.3 11.3 2.8 8.9 2.8 8c0-.3.1-.5.2-.5Z",
  instagram: "M8 2h8a6 6 0 0 1 6 6v8a6 6 0 0 1-6 6H8a6 6 0 0 1-6-6V8a6 6 0 0 1 6-6Zm0 2a4 4 0 0 0-4 4v8a4 4 0 0 0 4 4h8a4 4 0 0 0 4-4V8a4 4 0 0 0-4-4H8Zm4 3a5 5 0 1 1 0 10 5 5 0 0 1 0-10Zm0 2a3 3 0 1 0 0 6 3 3 0 0 0 0-6Zm5.5-3.2a1.2 1.2 0 1 1 0 2.4 1.2 1.2 0 0 1 0-2.4Z",
  chat: "M4 4h16a2 2 0 0 1 2 2v9a2 2 0 0 1-2 2H9l-5 4v-4a2 2 0 0 1-2-2V6a2 2 0 0 1 2-2Zm3 6h.01M12 10h.01M17 10h.01",
};

function SocialIcon({ path }: { path: string }) {
  return <svg viewBox="0 0 24 24" aria-hidden="true"><path d={path} /></svg>;
}

export function StoreFooter() {
  return <footer className="store-footer">
    <div className="store-footer-grid">
      <section className="store-footer-connect">
        <p className="store-footer-eyebrow">Фикусин рядом</p>
        <h2>Давайте найдём<br/>ваше растение</h2>
        <p className="store-footer-intro">Подскажем с выбором, доставкой и уходом — по-человечески и без долгого ожидания.</p>
        <div className="store-footer-contact-actions"><a className="store-footer-chat" href="https://t.me/ficusin62" target="_blank" rel="noreferrer"><i><SocialIcon path={socialIcons.chat}/></i><span><small>Ответим в Telegram</small>Написать в чат</span><b aria-hidden="true">→</b></a><a className="store-footer-phone" href="tel:+79156151100"><small>Позвонить в магазин</small>+7 915 615-11-00</a></div>
        <div className="store-footer-social-block"><p>Заглядывайте к нам</p><div className="store-footer-socials"><a className="vk" href="https://vk.ru/ficusin" target="_blank" rel="noreferrer" aria-label="Фикусин во ВКонтакте"><SocialIcon path={socialIcons.vk}/></a><a className="telegram" href="https://t.me/ficusin62" target="_blank" rel="noreferrer" aria-label="Фикусин в Telegram"><SocialIcon path={socialIcons.telegram}/></a><a className="instagram" href="https://www.instagram.com/ficusin_62/" target="_blank" rel="noreferrer" aria-label="Фикусин в Instagram"><SocialIcon path={socialIcons.instagram}/></a><a className="max-social" href="https://max.ru/channel_ficusin" target="_blank" rel="noreferrer" aria-label="Фикусин в MAX"><b>MAX</b></a></div></div>
      </section>
    </div>
    <div className="store-footer-bottom"><div className="store-footer-legal"><span>© Фикусин, {new Date().getFullYear()}</span><a href="/privacy">Политика</a><a href="/offer">Оферта</a></div><address className="store-footer-address"><a href="mailto:info@ficusin.ru">info@ficusin.ru</a><a href="/contacts">Рязань, Новосёлов, 40А</a></address></div>
  </footer>;
}
