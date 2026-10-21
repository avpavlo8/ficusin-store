import { useState, type ReactNode } from "react";

type OrderLampProps = {
  label: string;
  amount: ReactNode;
  children: ReactNode;
};

export function OrderLamp({ label, amount, children }: OrderLampProps) {
  const [lampOn, setLampOn] = useState(true);
  return <div className={`order-lamp-scene ${lampOn ? "is-lit" : ""}`}>
    <img className="cart-pendant-lamp" src="/assets/redesign/cart-pendant-lamp-v2.png" alt="" aria-hidden="true" width="600" height="600" />
    <button type="button" className="order-lamp-switch" onClick={() => setLampOn((current) => !current)} aria-pressed={lampOn} aria-label={lampOn ? "Выключить подсветку итога" : "Включить подсветку итога"} title="Нажмите на лампу, чтобы переключить свет">
      <svg viewBox="0 0 24 24" aria-hidden="true" fill="none" stroke="currentColor" strokeWidth="1.7" strokeLinecap="round"><path d="M12 3v8M6.4 5.6a8 8 0 1 0 11.2 0" /></svg>
    </button>
    <span className="order-lamp-beam" aria-hidden="true" />
    <div className="order-lamp-total"><span>{label}</span><strong>{amount}</strong></div>
    {children}
  </div>;
}
