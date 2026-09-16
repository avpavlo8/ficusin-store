from pathlib import Path

service = Path('backend/internal/procurement/service.go')
text = service.read_text()
old = '''\tif kind == "receipt" && service.executor != nil && service.executor.Configured("saby") {\n\t\tif refresher, ok := service.executor.(SabyCatalogRefresher); ok {\n\t\t\tif _, err := refresher.RefreshSabyCatalog(ctx); err != nil {\n\t\t\t\treturn ActionBatch{}, &UserFacingError{Message: "Не удалось обновить актуальные остатки СБИС перед поступлением. Повторите подготовку поступления позже."}\n\t\t\t}\n\t\t}\n\t}\n'''
new = '''\tif kind == "receipt" && service.executor != nil && service.executor.Configured("saby") {\n\t\tif refresher, ok := service.executor.(SabyCatalogRefresher); ok {\n\t\t\t// Полная синхронизация каталога СБИС может занимать десятки секунд.\n\t\t\t// Нельзя держать HTTP-запрос подготовки поступления до её окончания:\n\t\t\t// reverse proxy обрывает такой запрос с 502. Даём короткое окно для\n\t\t\t// свежих остатков, но при таймауте/ошибке продолжаем с локальным\n\t\t\t// снимком; фоновая синхронизация всё равно обновит каталог отдельно.\n\t\t\trefreshCtx, cancel := context.WithTimeout(ctx, 8*time.Second)\n\t\t\t_, _ = refresher.RefreshSabyCatalog(refreshCtx)\n\t\t\tcancel()\n\t\t}\n\t}\n'''
if old not in text:
    raise SystemExit('target block not found')
service.write_text(text.replace(old, new, 1))

test = Path('backend/internal/procurement/service_prepare_receipt_fallback_test.go')
test.write_text(r'''package procurement

import (
    "context"
    "errors"
    "testing"
)

type failingSabyReceiptExecutor struct{}

func (failingSabyReceiptExecutor) Configured(channel string) bool { return channel == "saby" }
func (failingSabyReceiptExecutor) Execute(context.Context, ActionItem) (ActionExecution, error) {
    return ActionExecution{}, nil
}
func (failingSabyReceiptExecutor) RefreshSabyCatalog(context.Context) (ChannelLinkResult, error) {
    return ChannelLinkResult{}, errors.New("temporary Saby timeout")
}

func TestPrepareReceiptFallsBackToCachedSabySnapshot(t *testing.T) {
    store := &storeStub{}
    service := NewServiceWithExecutor(store, failingSabyReceiptExecutor{})
    batch, err := service.PrepareBatch(context.Background(), Actor{}, 18, "receipt", []string{"saby_receipt"})
    if err != nil {
        t.Fatalf("PrepareBatch must not fail only because Saby catalog refresh failed: %v", err)
    }
    if batch.Kind != "receipt" {
        t.Fatalf("batch=%+v", batch)
    }
}
''')
