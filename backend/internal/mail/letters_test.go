package mail

import (
	"strings"
	"testing"
)

func sampleOrder() OrderLetter {
	return OrderLetter{
		Number:       "0001-15",
		CustomerName: "Александр Павлов",
		Items:        []OrderLine{{Name: "Фикус Бенджамина", Price: 1200, Quantity: 2}},
		Subtotal:     2400,
		DeliveryFee:  515,
		Total:        2915,
		Delivery:     "cdek",
		Address:      "Москва, Ленина, 1",
	}
}

// An unpaid order must carry the way to pay it. A confirmation that says
// nothing about money leaves the customer waiting for a letter that never
// comes, and the shop waiting for money that never arrives.
func TestConfirmationOfAnUnpaidOrderCarriesThePaymentLink(t *testing.T) {
	order := sampleOrder()
	order.PaymentStatus = "pending"
	order.PayURL = "https://yoomoney.ru/checkout/pay/123"

	letter := Confirmation(order)

	if !strings.Contains(letter.Body, order.PayURL) {
		t.Fatalf("в письме нет ссылки на оплату:\n%s", letter.Body)
	}
}

// When the manager still has to price the delivery there is nothing to pay
// yet, so the letter must promise a link rather than show a total that would
// change.
func TestConfirmationExplainsWhenDeliveryIsNotPricedYet(t *testing.T) {
	order := sampleOrder()
	order.FeePending = true
	order.DeliveryFee = 0
	order.Total = 2400

	letter := Confirmation(order)

	if !strings.Contains(letter.Body, "рассчитает менеджер") {
		t.Fatalf("в письме нет объяснения про доставку:\n%s", letter.Body)
	}
	if !strings.Contains(letter.Body, "+ доставка") {
		t.Fatalf("итог должен показывать, что доставка ещё не в сумме:\n%s", letter.Body)
	}
}

// The tracking number is the whole point of the "shipped" letter.
func TestShippedLetterCarriesTheTrackingNumber(t *testing.T) {
	order := sampleOrder()
	order.TrackNumber = "1234567890"

	letter, send := StatusChange(order, "shipped")

	if !send {
		t.Fatal("письмо об отправке должно уходить")
	}
	if !strings.Contains(letter.Body, "1234567890") {
		t.Fatalf("в письме нет трек-номера:\n%s", letter.Body)
	}
}

// Internal steps are the manager's business. A shop that emails about every
// one of them gets filtered into spam, and then the letters that matter go
// there too.
func TestInternalStatusesSendNoLetter(t *testing.T) {
	for _, status := range []string{"new", "confirmed", "assembling", "неизвестный"} {
		if _, send := StatusChange(sampleOrder(), status); send {
			t.Fatalf("статус %s не должен порождать письмо", status)
		}
	}
}

func TestGreetingUsesTheFirstNameOrStaysNeutral(t *testing.T) {
	if greeting("Александр Павлов") != "Здравствуйте, Александр!" {
		t.Fatalf("обращение по имени сломалось: %s", greeting("Александр Павлов"))
	}
	if greeting("  ") != "Здравствуйте!" {
		t.Fatalf("без имени должно быть нейтральное обращение: %s", greeting("  "))
	}
}
