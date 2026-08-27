export function DevelopmentNotice() {
  return <aside className="development-notice" aria-label="Статус сайта">
    <p className="ui-container">
      <span className="development-notice-full">Сайт в режиме доработки. Каталог и оформление заказа работают, но отдельные страницы и характеристики мы ещё приводим в порядок. Если заметили ошибку — <a href="https://t.me/ficusin62" target="_blank" rel="noreferrer">напишите нам</a>.</span>
      <span className="development-notice-mobile">Сайт в доработке. Ошибка? <a href="https://t.me/ficusin62" target="_blank" rel="noreferrer">Напишите нам.</a></span>
    </p>
  </aside>;
}
