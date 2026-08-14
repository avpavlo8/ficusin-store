package integration

import "time"

// Список отправлений FBS третьей версии не отдаёт created_at — это поле
// осталось во второй и у FBO. Вместо него есть in_process_at (когда заказ
// взяли в работу), shipment_date и delivering_date. Поэтому у всех 6340
// отправлений дата была пустая, разбор даты падал, строка молча
// пропускалась — и канал говорил «Ozon не вернул отправлений», хотя площадка
// вернула их шесть тысяч. Разведка это и показала: доставленных 5654,
// позиций 6102, с кодом продавца 6102, а в расчёт попало ноль.
//
// Берём первую дату, которая есть: для потребности в закупке важен день
// продажи с точностью до суток, а не то, в какой момент жизни отправления
// её записали.
func (posting ozonPosting) saleDate() (time.Time, error) {
	return parseMarketplaceDate(firstNonEmpty(
		posting.CreatedAt, posting.InProcessAt, posting.ShipmentDate, posting.DeliveringDate,
	))
}
