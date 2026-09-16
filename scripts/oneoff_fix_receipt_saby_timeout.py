from pathlib import Path


def replace_once(path: str, old: str, new: str) -> None:
    p = Path(path)
    text = p.read_text()
    if old not in text:
        raise SystemExit(f"pattern not found in {path}: {old[:120]!r}")
    p.write_text(text.replace(old, new, 1))


replace_once(
    "backend/internal/procurement/service.go",
    '''\tif kind == "receipt" && service.executor != nil && service.executor.Configured("saby") {\n\t\tif refresher, ok := service.executor.(SabyCatalogRefresher); ok {\n\t\t\tif _, err := refresher.RefreshSabyCatalog(ctx); err != nil {\n\t\t\t\treturn ActionBatch{}, &UserFacingError{Message: "Не удалось обновить актуальные остатки СБИС перед поступлением. Повторите подготовку поступления позже."}\n\t\t\t}\n\t\t}\n\t}\n''',
    '''\t// Receipt preparation snapshots the local Saby mirror only. The browser\n\t// explicitly queues a catalogue refresh and waits for the catalogue worker\n\t// before calling this endpoint. Never perform external Saby I/O inside this\n\t// HTTP request: a full catalogue traversal can exceed the reverse-proxy\n\t// timeout and turn a healthy operation into a 502.\n''',
)

replace_once(
    "backend/internal/procurement/service_test.go",
    '''func TestPrepareReceiptRefreshesSabyBalancesBeforeSnapshot(t *testing.T) {\n\tt.Parallel()\n\tstore := &storeStub{}\n\texecutor := &sabyRefreshExecutorStub{}\n\tservice := NewServiceWithExecutor(store, executor)\n\tbatch, err := service.PrepareBatch(context.Background(), Actor{}, 18, "receipt", nil)\n\tif err != nil {\n\t\tt.Fatal(err)\n\t}\n\tif batch.Kind != "receipt" || executor.refreshCalls != 1 {\n\t\tt.Fatalf("batch=%+v refreshCalls=%d", batch, executor.refreshCalls)\n\t}\n}\n''',
    '''func TestPrepareReceiptDoesNotReadSabySynchronously(t *testing.T) {\n\tt.Parallel()\n\tstore := &storeStub{}\n\texecutor := &sabyRefreshExecutorStub{}\n\tservice := NewServiceWithExecutor(store, executor)\n\tbatch, err := service.PrepareBatch(context.Background(), Actor{}, 18, "receipt", nil)\n\tif err != nil {\n\t\tt.Fatal(err)\n\t}\n\tif batch.Kind != "receipt" || executor.refreshCalls != 0 {\n\t\tt.Fatalf("batch=%+v refreshCalls=%d", batch, executor.refreshCalls)\n\t}\n}\n''',
)

replace_once(
    "frontend/src/AdminProcurementDialogs.tsx",
    '''import type { NomenclatureCandidate, ProcurementActionBatch, ProcurementActionItem, ProcurementAlias, ProcurementOrder, ProcurementOrderDetail, ProcurementOrderLine, ProcurementRecommendation, ProcurementSupplier, ProcurementSettings } from "./adminTypes";''',
    '''import type { IntegrationSyncStatus, NomenclatureCandidate, ProcurementActionBatch, ProcurementActionItem, ProcurementAlias, ProcurementOrder, ProcurementOrderDetail, ProcurementOrderLine, ProcurementRecommendation, ProcurementSupplier, ProcurementSettings } from "./adminTypes";''',
)

replace_once(
    "frontend/src/AdminProcurementDialogs.tsx",
    '''  const prepare = async (kind: "receipt" | "prices") => {\n    const selected = kind === "prices" ? Object.entries(priceChannels).filter(([, enabled]) => enabled).map(([channel]) => channel) : [];\n    const apiChannels = selected.filter((channel) => channel !== "saby_price");\n    setSaving(true);\n    try {\n      if (selected.includes("saby_price")) await downloadSabyPrices();\n      if (kind === "receipt" || apiChannels.length > 0) await api(`/api/v1/admin/procurement/orders/${orderId}/batches`, { method: "POST", body: JSON.stringify({ kind, channels: apiChannels }) });\n      await load();\n    } catch (error) { onError((error as Error).message); } finally { setSaving(false); }\n  };''',
    '''  const refreshSabyBeforeReceipt = async () => {\n    const requestedAt = Date.now();\n    await api("/api/v1/admin/procurement/integrations/saby/catalog", { method: "POST" });\n    const deadline = requestedAt + 90_000;\n    while (Date.now() < deadline) {\n      const snapshot = await api<{ integrationSync: IntegrationSyncStatus[] }>("/api/v1/admin/procurement", { cache: "no-store" });\n      const lane = snapshot.integrationSync?.find((item) => item.channel === "saby" && item.resource === "catalog");\n      if (lane?.status === "error") throw new Error(lane.lastError || "Не удалось обновить остатки СБИС");\n      const succeededAt = lane?.lastSuccessAt ? new Date(lane.lastSuccessAt).getTime() : 0;\n      if (lane?.status === "ok" && lane.completedGeneration >= lane.requestedGeneration && succeededAt >= requestedAt - 2_000) return;\n      await new Promise((resolve) => window.setTimeout(resolve, 1_500));\n    }\n    throw new Error("СБИС ещё обновляет остатки. Подождите немного и повторите подготовку поступления.");\n  };\n  const prepare = async (kind: "receipt" | "prices") => {\n    const selected = kind === "prices" ? Object.entries(priceChannels).filter(([, enabled]) => enabled).map(([channel]) => channel) : [];\n    const apiChannels = selected.filter((channel) => channel !== "saby_price");\n    setSaving(true);\n    try {\n      if (kind === "receipt") await refreshSabyBeforeReceipt();\n      if (selected.includes("saby_price")) await downloadSabyPrices();\n      if (kind === "receipt" || apiChannels.length > 0) await api(`/api/v1/admin/procurement/orders/${orderId}/batches`, { method: "POST", body: JSON.stringify({ kind, channels: apiChannels }) });\n      await load();\n    } catch (error) { onError((error as Error).message); } finally { setSaving(false); }\n  };''',
)
