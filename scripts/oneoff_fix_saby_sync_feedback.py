from pathlib import Path
import re

p = Path('frontend/src/AdminProcurement.tsx')
text = p.read_text()
pattern = re.compile(r'  const syncCatalog = async \(channel: string\) => \{.*?\n  \};\n  const checkIntegration = async', re.S)
replacement = '''  const syncCatalog = async (channel: string) => {
    setSyncingCatalog(channel);
    setIntegrationNotice({ channel, ok: true, text: channel === "saby" ? "Запрашиваем обновление каталога СБИС…" : `Запрашиваем обновление ${integrationChannelLabel(channel)}…` });
    try {
      const result = await api<{ link: { fetched: number; linked: number; unmatched: number; channelKeys: number; catalogKeys: number; channelSamples: string[]; catalogSamples: string[]; queued?: boolean; queueStatus?: string; nextAttemptAt?: string } }>(`/api/v1/admin/procurement/integrations/${channel}/catalog`, { method: "POST" });
      const link = result.link;
      if (link.queued && channel === "saby") {
        setIntegrationNotice({ channel, ok: true, text: "Запрос принят. Ждём, пока СБИС отдаст каталог…" });
        let targetGeneration = 0;
        const deadline = Date.now() + 120000;
        while (Date.now() < deadline) {
          await new Promise((resolve) => window.setTimeout(resolve, 2000));
          const snapshot = await api<ProcurementData>("/api/v1/admin/procurement");
          setData(snapshot);
          const state = snapshot.integrationSync.find((item) => item.channel === "saby" && item.resource === "catalog");
          if (!state) continue;
          if (!targetGeneration) targetGeneration = state.requestedGeneration;
          if (state.status === "error") {
            setIntegrationNotice({ channel, ok: false, text: `СБИС: обновление не выполнено${state.lastError ? ` — ${state.lastError}` : "."}` });
            return;
          }
          if (state.completedGeneration >= targetGeneration && state.status !== "running" && state.status !== "queued") {
            setIntegrationNotice({ channel, ok: true, text: `Каталог СБИС обновлён: ${state.rowsSynced} позиций. Данные на странице перечитаны.` });
            return;
          }
          setIntegrationNotice({
            channel,
            ok: true,
            text: state.status === "running"
              ? "СБИС: загружаем каталог…"
              : "СБИС: обновление стоит в очереди…",
          });
        }
        setIntegrationNotice({ channel, ok: false, text: "СБИС: обновление не завершилось за 2 минуты. Откройте «Интеграции» — там будет видна причина или состояние очереди." });
        return;
      }
      if (link.queued) {
        setIntegrationNotice({ channel, ok: true, text: `Текущее зеркало сопоставлено; обновление ${integrationChannelLabel(channel)} добавлено в общую очередь.` });
        await load();
        return;
      }
      if (channel === "saby") {
        setIntegrationNotice({ channel, ok: true, text: `Справочник СБИС обновлён: ${link.fetched} позиций. Новые карточки уже доступны для сопоставления.` });
        await load();
        return;
      }
      const detail = link.linked === 0 && link.fetched > 0
        ? ` Сравнивали ${link.channelKeys} ключей канала с ${link.catalogKeys} ключами СБИС. У канала: ${(link.channelSamples || []).join(", ") || "нет"}. В СБИС: ${(link.catalogSamples || []).join(", ") || "нет"}.`
        : "";
      setIntegrationNotice({ channel, ok: link.linked > 0, text: `Прочитано карточек: ${link.fetched}, связано: ${link.linked}, без совпадения: ${link.unmatched}.${detail}` });
      await load();
    } catch (error) {
      setIntegrationNotice({ channel, ok: false, text: (error as Error).message });
    } finally {
      setSyncingCatalog("");
    }
  };
  const checkIntegration = async'''
new_text, count = pattern.subn(replacement, text, count=1)
if count != 1:
    raise SystemExit(f'syncCatalog block replacements={count}')
text = new_text
needle = '''    </div>\n\n    <div className="procurement-tabs" role="tablist">'''
insert = '''    </div>\n    {integrationNotice?.channel === "saby" && <p className={`integration-check-result ${integrationNotice.ok ? "success" : "error"}`} role="status">{integrationNotice.text}</p>}\n\n    <div className="procurement-tabs" role="tablist">'''
if needle not in text:
    raise SystemExit('toolbar insertion point not found')
text = text.replace(needle, insert, 1)
p.write_text(text)
