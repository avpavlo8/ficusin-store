package payment

import (
	"testing"

	"github.com/avpavlo8/ficusin-store/backend/internal/integration"
)

// Отмена платежа подключена приведением типа, а не строкой в интерфейсе
// provider — так провайдер без отмены остаётся рабочим. Цена этого решения:
// если подпись CancelPayment когда-нибудь уедет, приведение перестанет
// срабатывать молча и платежи так же молча перестанут гаситься. Эта
// строка превращает такую поломку в ошибку компиляции.
var _ canceller = (*integration.YooKassaClient)(nil)

// Провайдер, который не умеет отменять, должен оставаться рабочим
// провайдером: интерфейс provider о отмене ничего не знает.
func TestCancelIsOptionalCapability(t *testing.T) {
	var client any = (*integration.YooKassaClient)(nil)
	if _, ok := client.(canceller); !ok {
		t.Fatal("ЮKassa перестала уметь отменять платежи — автоотмена перестанет их гасить")
	}
}
