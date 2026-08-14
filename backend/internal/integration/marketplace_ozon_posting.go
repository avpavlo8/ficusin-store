package integration

// Структура отправления живёт рядом с выбором даты: одно без другого
// читается неверно — именно на этом разрыве шесть тысяч отправлений
// превращались в ноль продаж.
type ozonPosting struct {
	CreatedAt      string `json:"created_at"`
	InProcessAt    string `json:"in_process_at"`
	ShipmentDate   string `json:"shipment_date"`
	DeliveringDate string `json:"delivering_date"`
	Status         string `json:"status"`
	Products       []struct {
		OfferID  string `json:"offer_id"`
		Quantity int    `json:"quantity"`
		Price    string `json:"price"`
	} `json:"products"`
}
