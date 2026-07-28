import { LegalPage } from "../legal-page";

export default function RequisitesPage() {
  return (
    <LegalPage eyebrow="Продавец" title="Контакты и реквизиты">
      <section>
        <h2>Магазин «Фикусин»</h2>
        <dl className="requisites-list">
          <div><dt>Продавец</dt><dd>Индивидуальный предприниматель Павловский Александр Владимирович</dd></div>
          <div><dt>ИНН</dt><dd>620201228029</dd></div>
          <div><dt>ОГРНИП</dt><dd>324620000031276</dd></div>
          <div><dt>Юридический адрес</dt><dd>390047, Рязанская область, Рязанский район, д. Вишневка, 2-й Вишнёвый проезд, д. 1</dd></div>
          <div><dt>Магазин и самовывоз</dt><dd>г. Рязань, ул. Новосёлов, д. 40А</dd></div>
          <div><dt>Телефон магазина</dt><dd><a href="tel:+79156151100">+7 915 615-11-00</a></dd></div>
          <div><dt>Telegram</dt><dd><a href="https://t.me/ficusin62" target="_blank" rel="noreferrer">@ficusin62</a></dd></div>
          <div><dt>Режим работы</dt><dd>ежедневно, 08:00–20:00</dd></div>
        </dl>
      </section>
      <aside className="legal-callout">Заказы через сайт принимаются круглосуточно. Обработка заказов и ответы покупателям — в часы работы магазина.</aside>
    </LegalPage>
  );
}
