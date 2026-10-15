import { useEffect, useRef, useState } from "react";
import { api } from "./adminShared";
import type { NomenclatureCandidate, ProcurementRequest } from "./adminTypes";
import { useProcurementDialog, useProcurementError } from "./procurementUX";

export function ProcurementRequestForm({ onClose, onSaved, onError: externalError, request, initialName = "", initialNotes = "" }: { onClose: () => void; onSaved: () => void; onError: (value: string) => void; request?: ProcurementRequest; initialName?: string; initialNotes?: string }) {
  const {error:dialogError,errorRef,notify:onError,clear:clearError}=useProcurementError(externalError);
  const [kind, setKind] = useState(request?.kind || "customer_order");
  const [customerOrderId, setCustomerOrderId] = useState(String(request?.customerOrderId || ""));
  const [name, setName] = useState(request?.requestedName || initialName);
  const [sabyId, setSabyId] = useState(request?.sabyId || "");
  const [quantity, setQuantity] = useState(String(request?.quantity || 1));
  const [notes, setNotes] = useState(request?.notes || initialNotes);
  const [dueDate, setDueDate] = useState(request?.dueDate || "");
  const [assignee, setAssignee] = useState(request?.assignee || "");
  const [saving, setSaving] = useState(false);
  const [submitted, setSubmitted] = useState(false);
  const [candidates, setCandidates] = useState<NomenclatureCandidate[]>([]);
  const nameRef = useRef<HTMLInputElement>(null);
  const customerOrderRef=useRef<HTMLInputElement>(null);
  const customerOrderError = kind === "customer_order" && customerOrderId !== "" && (!Number.isSafeInteger(Number(customerOrderId)) || Number(customerOrderId) < 1);
  const quantityRef = useRef<HTMLInputElement>(null);
  const dirty = name !== (request?.requestedName || initialName) || sabyId !== (request?.sabyId || "") || quantity !== String(request?.quantity || 1) || notes !== (request?.notes || initialNotes) || dueDate !== (request?.dueDate || "") || assignee !== (request?.assignee || "") || kind !== (request?.kind || "customer_order") || customerOrderId !== String(request?.customerOrderId || "");
  const close = () => { if (!saving && (!dirty || window.confirm("Закрыть форму без сохранения изменений?"))) onClose(); };
  const dialogRef = useProcurementDialog(close);
  const quantityError = !Number.isInteger(Number(quantity)) || Number(quantity) < 1 || Number(quantity) > 1000000;
  useEffect(() => {
    if (name.trim().length < 2 || sabyId) { setCandidates([]); return; }
    let cancelled = false;
    const timer = window.setTimeout(() => api<{ items: NomenclatureCandidate[] }>(`/api/v1/admin/procurement/nomenclature?q=${encodeURIComponent(name.trim())}`).then((result) => { if (!cancelled) setCandidates(result.items.slice(0, 6)); }).catch(() => { if (!cancelled) setCandidates([]); }), 250);
    return () => { cancelled = true; window.clearTimeout(timer); };
  }, [name, sabyId]);
  const save = async () => {
    setSubmitted(true);
    if (!name.trim()) { nameRef.current?.focus(); return; }
    if (quantityError) { quantityRef.current?.focus(); return; }
    if(customerOrderError){customerOrderRef.current?.focus();return;}
    setSaving(true);
    try {
      await api(request ? `/api/v1/admin/procurement/requests/${request.id}` : "/api/v1/admin/procurement/requests", { method: request ? "PATCH" : "POST", body: JSON.stringify({ kind, requestedName: name, sabyId, quantity: Number(quantity), notes, dueDate, assignee, status: request?.status || "open", source: "manual", customerOrderId: kind === "customer_order" && customerOrderId ? Number(customerOrderId) : null }) });
      onSaved();
    } catch (error) { onError((error as Error).message); } finally { setSaving(false); }
  };
  return <><button tabIndex={-1} className="admin-dialog-backdrop" aria-label="Закрыть" onClick={close} /><div ref={dialogRef} className="admin-dialog" role="dialog" aria-modal="true" aria-labelledby="request-title"><header><h2 id="request-title">{request ? "Редактировать запрос" : "Добавить к закупке"}</h2><button onClick={close} disabled={saving} aria-label="Закрыть">×</button></header>{dialogError && <div ref={errorRef} tabIndex={-1} className="procurement-dialog-error" role="alert"><strong>Не удалось сохранить запрос</strong><span>{dialogError}</span><button aria-label="Закрыть сообщение об ошибке" onClick={clearError}>×</button></div>}<form onSubmit={(event) => { event.preventDefault(); void save(); }} noValidate><div className="admin-form-grid">
    <label>Тип<select value={kind} disabled={Boolean(request)} onChange={(event) => setKind(event.target.value)}><option value="customer_order">Заказ клиента</option><option value="staff_recommendation">Рекомендация магазина</option></select></label>
    <label>Количество, шт.<input aria-label="Количество, шт." ref={quantityRef} aria-invalid={quantityError && (submitted || quantity !== "1")} aria-describedby={quantityError ? "request-quantity-error" : undefined} type="text" inputMode="numeric" value={quantity} onChange={(event) => setQuantity(event.target.value)} />{quantityError && <small id="request-quantity-error" className="procurement-warning">Введите целое число от 1 до 1 000 000</small>}</label>
    {kind === "customer_order" && <label>Номер заказа в CRM<input ref={customerOrderRef} aria-label="Номер заказа в CRM" aria-invalid={customerOrderError} type="number" min="1" step="1" disabled={Boolean(request)} value={customerOrderId} onChange={(event) => setCustomerOrderId(event.target.value)} placeholder="Необязательно" />{customerOrderError && <small className="procurement-warning">Введите целый положительный номер заказа</small>}</label>}
    <label>Нужно к дате<input type="date" value={dueDate} onChange={(event) => setDueDate(event.target.value)} /></label><label>Ответственный<input maxLength={120} value={assignee} onChange={(event) => setAssignee(event.target.value)} placeholder="Имя сотрудника" /></label>
    <label className="wide">Название товара<input aria-label="Название товара" aria-describedby={submitted && !name.trim() ? "request-name-error" : undefined} ref={nameRef} aria-invalid={submitted && !name.trim()} value={name} onChange={(event) => { setName(event.target.value); setSabyId(""); }} placeholder="Начните вводить название" />{submitted && !name.trim() && <small id="request-name-error" className="procurement-warning">Укажите название товара</small>}</label>
    {sabyId ? <div className="wide procurement-selected-product"><strong>Связано с товаром СБИС</strong><button type="button" onClick={() => setSabyId("")}>Изменить</button></div> : candidates.length > 0 && <div className="wide procurement-request-candidates">{candidates.map((item) => <button type="button" key={item.sabyId} onClick={() => { setSabyId(item.sabyId); setName(item.name); }}><strong>{item.name}</strong><small>{item.code || item.article}</small></button>)}</div>}
    <label className="wide">Комментарий<textarea aria-label="Комментарий" value={notes} onChange={(event) => setNotes(event.target.value)} placeholder="Пожелания клиента и детали закупки" /></label>
    {request && <p className="wide admin-hint">Создан {new Date(request.createdAt).toLocaleString("ru-RU")}. Изменения записываются в журнал действий. Уже распределённое количество защищено от уменьшения.</p>}
  </div><div className="dialog-actions"><button type="button" onClick={close} disabled={saving}>Отмена</button><button type="submit" className="primary" disabled={saving}>{saving ? "Сохраняем…" : request ? "Сохранить" : "Добавить"}</button></div></form></div></>;
}
