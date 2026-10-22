import { useEffect, useState } from "react";
import { apiJSON } from "./lib/api";
import { chatRequest } from "./lib/chat-api";
import { isIOS, isStandalone, pushSupported } from "./lib/pwa";

function applicationKey(value: string) {
  const raw = atob(value.replace(/-/g, '+').replace(/_/g, '/').padEnd(Math.ceil(value.length / 4) * 4, '='));
  return Uint8Array.from(raw, character => character.charCodeAt(0));
}
async function registration() {
  await navigator.serviceWorker.register('/sw.js', { updateViaCache: 'none' });
  return navigator.serviceWorker.ready;
}

export default function ChatNotifications({ staff = false }: { staff?: boolean }) {
  const base = `/api/v1/${staff ? 'admin/' : ''}chat/notifications`;
  const [key, setKey] = useState('');
  const [subscribed, setSubscribed] = useState(false);
  const [busy, setBusy] = useState(false);
  const [hint, setHint] = useState('');
  const [error, setError] = useState('');
  useEffect(() => {
    let active = true;
    const refresh = async () => {
      try {
        const config = await apiJSON<{ publicKey: string; enabled: boolean }>('/api/v1/push/key');
        if (!active || !config.enabled) return;
        setKey(config.publicKey);
        if (!pushSupported() || Notification.permission !== 'granted') return;
        const subscription = await (await registration()).pushManager.getSubscription();
        const state = subscription ? await chatRequest<{ subscribed: boolean }>(base + '/state', { endpoint: subscription.endpoint }) : { subscribed: false };
        if (active) setSubscribed(state.subscribed);
      } catch { /* The bell can retry enabling without interrupting the conversation. */ }
    };
    void refresh(); window.addEventListener('ficusin-chat-push-changed', refresh);
    return () => { active = false; window.removeEventListener('ficusin-chat-push-changed', refresh); };
  }, [base]);
  const toggle = async () => {
    setHint(''); setError('');
    if (isIOS() && !isStandalone()) { setHint('На iPhone добавьте Фикусин на экран «Домой» через меню «Поделиться», откройте его оттуда и включите уведомления.'); return; }
    if (!pushSupported()) { setHint('Этот браузер не поддерживает push-уведомления. Откройте сайт в Safari или Chrome.'); return; }
    setBusy(true);
    try {
      // Request inside the tap gesture; Safari rejects prompts after a network wait.
      if (!subscribed && await Notification.requestPermission() !== 'granted') throw new Error('Разрешите уведомления для Фикусина в настройках браузера.');
      const manager = (await registration()).pushManager;
      const subscription = await manager.getSubscription() || (!subscribed ? await manager.subscribe({ userVisibleOnly: true, applicationServerKey: applicationKey(key) }) : null);
      if (subscription) await chatRequest(base, subscribed ? { endpoint: subscription.endpoint } : subscription.toJSON(), subscribed ? 'DELETE' : 'POST');
      setSubscribed(!subscribed); setHint(subscribed ? 'Уведомления о сообщениях выключены.' : 'Уведомления включены. Покажем текст новых сообщений, даже если сайт закрыт.');
      window.dispatchEvent(new Event('ficusin-chat-push-changed'));
    } catch (caught) { setError(caught instanceof Error && /[А-Яа-я]/.test(caught.message) ? caught.message : 'Не удалось изменить уведомления. Проверьте соединение и попробуйте ещё раз.'); }
    finally { setBusy(false); }
  };
  if (!key) return null;
  return <div className="chat-notifications"><button type="button" className="chat-notification-toggle" aria-label={subscribed ? 'Выключить уведомления о сообщениях' : 'Включить уведомления о сообщениях'} aria-pressed={subscribed} disabled={busy} title={subscribed ? 'Уведомления включены' : 'Уведомления о сообщениях'} onClick={() => void toggle()}><svg viewBox="0 0 24 24" width="20" height="20" fill="none" stroke="currentColor" strokeWidth="1.7" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true"><path d="M18 8a6 6 0 0 0-12 0c0 7-3 7-3 9h18c0-2-3-2-3-9M10 21h4" />{!subscribed && <path d="m3 3 18 18" />}</svg></button>{(hint || error) && <div className="chat-notification-hint" role={error ? 'alert' : 'status'}><p>{error || hint}</p><button type="button" aria-label="Закрыть подсказку об уведомлениях" onClick={() => { setHint(''); setError(''); }}>Понятно</button></div>}</div>;
}
