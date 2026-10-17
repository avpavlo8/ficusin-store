import type { Dispatch, FormEvent, SetStateAction } from "react";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import type { CartLine } from "./CartCheckout";
import { normalizeRussianPhone } from "./lib/phone";
import { discountedPrice } from "./lib/checkoutPrice";
import { readCheckoutDraft, saveCheckoutDraft, clearCheckoutDraft } from "./lib/checkoutDraft";
import { getAttribution, track } from "./lib/analytics";

type Cart = Record<string, number>;
export type CheckoutProfile = {
  name: string;
  phone: string;
  email: string;
  address: string;
};
type PaymentMethod = { id: string; title: string; note: string };
type CdekCity = { code: number; city: string; region?: string };
type CdekOffice = {
  code: string;
  name: string;
  location: { city: string; address: string; address_full?: string };
  work_time?: string;
};
type CdekQuote = {
  tariffCode: number;
  tariffName: string;
  price: number;
  daysMin: number;
  daysMax: number;
};
const baseDeliveryOptions = [
  { id: "pickup", title: "Самовывоз в Рязани", detail: "Бесплатно", fee: 0 },
  { id: "cdek", title: "СДЭК по России", detail: "Рассчитаем по адресу", fee: null },
] as const;

type UseCheckoutArgs = {
  cartLines: CartLine[];
  cartCount: number;
  setCart: Dispatch<SetStateAction<Cart>>;
  setNotice: Dispatch<SetStateAction<string>>;
  discountBps: number;
  initialOpen?: boolean;
};

export function useCheckout({ cartLines, cartCount, setCart, setNotice, discountBps, initialOpen = false }: UseCheckoutArgs) {
  const [draft] = useState(readCheckoutDraft);
  const [draftOwner, setDraftOwner] = useState(draft?.owner ?? "guest");
  const [step, setStep] = useState<1 | 2 | 3>(draft?.step ?? 2);
  const [deliveryConfirmed, setDeliveryConfirmed] = useState(draft?.deliveryConfirmed ?? false);
  const [orderError, setOrderError] = useState("");
  const pendingRestoreOffice = useRef(draft?.officeCode ?? "");
  const submitLock = useRef(false);
  const quoteRequest = useRef(0);
  const [methodsRevision, setMethodsRevision] = useState(0);
  const [checkoutOpen, setCheckoutOpen] = useState(initialOpen);
  const [delivery, setDelivery] = useState(draft?.delivery ?? "pickup");
  const [submitting, setSubmitting] = useState(false);
  const [orderNumber, setOrderNumber] = useState("");
  const [orderConfirmationPending, setOrderConfirmationPending] = useState(false);
  const [cdekCityQuery, setCdekCityQuery] = useState(draft?.city?.city ?? "");
  const [cdekCities, setCdekCities] = useState<CdekCity[]>([]);
  const [cdekCity, setCdekCity] = useState<CdekCity | null>(draft?.city ?? null);
  const [cdekOffices, setCdekOffices] = useState<CdekOffice[]>([]);
  const [cdekOfficeCode, setCdekOfficeCode] = useState("");
  const [cdekOfficeQuery, setCdekOfficeQuery] = useState("");
  const [cdekOfficeListOpen, setCdekOfficeListOpen] = useState(false);
  const [cdekQuotes, setCdekQuotes] = useState<CdekQuote[]>([]);
  const [cdekTariffCode, setCdekTariffCode] = useState(0);
  const [cdekRepack, setCdekRepack] = useState(draft?.repack ?? false);
  const [paymentMethods, setPaymentMethods] = useState<PaymentMethod[]>([]);
  const [paymentMethod, setPaymentMethod] = useState(draft?.paymentMethod ?? "online");
  const [cdekLoading, setCdekLoading] = useState(false);
  const [cdekError, setCdekError] = useState("");
  const [cdekAvailable, setCdekAvailable] = useState(true);
  const [checkoutProfile, setCheckoutProfile] = useState<CheckoutProfile>(draft?.profile ?? {
    name: "",
    phone: "",
    email: "",
    address: "",
  });

  const applyCustomerProfile = useCallback((profile: CheckoutProfile | null, owner: string) => {
    setDraftOwner(owner);
    const empty = { name: "", phone: "", email: "", address: "" };
    const changedOwner = (draft?.owner ?? "guest") !== owner;
    setCheckoutProfile(current => changedOwner ? profile ?? empty : {
      name: current.name || profile?.name || "", phone: current.phone || profile?.phone || "",
      email: current.email || profile?.email || "", address: current.address || profile?.address || "",
    });
    if (changedOwner) { clearCheckoutDraft(); setStep(2); }
  }, [draft]);

  useEffect(() => {
    let cancelled = false;
    fetch(`/api/v1/payments/methods?delivery=${encodeURIComponent(delivery)}`, {
      credentials: "same-origin",
      cache: "no-store",
    })
      .then((response) => response.json())
      .then((data: { methods?: PaymentMethod[] }) => {
        if (cancelled) return;
        const methods = data.methods ?? [];
        setPaymentMethods(methods);
        setPaymentMethod((current) =>
          methods.some((item) => item.id === current) ? current : (methods[0]?.id ?? "online"),
        );
      })
      .catch(() => {
        if (!cancelled) setPaymentMethods([]);
      });
    return () => {
      cancelled = true;
    };
  }, [delivery, methodsRevision]);

  useEffect(() => {
    fetch("/api/v1/delivery/cdek?action=status", { cache: "no-store" })
      .then((response) => response.json())
      .then((data: { available?: boolean }) => setCdekAvailable(Boolean(data.available)))
      .catch(() => setCdekAvailable(false));
  }, []);


  useEffect(() => {
    if (
      delivery !== "cdek" ||
      cdekCityQuery.trim().length < 2 ||
      cdekCityQuery.trim() === cdekCity?.city
    ) {
      return;
    }
    const controller = new AbortController();
    const timer = window.setTimeout(async () => {
      try {
        setCdekLoading(true);
        setCdekError("");
        const response = await fetch(
          `/api/v1/delivery/cdek?action=cities&city=${encodeURIComponent(cdekCityQuery.trim())}`,
          { signal: controller.signal },
        );
        const data = (await response.json()) as { cities?: CdekCity[]; error?: string };
        if (!response.ok) throw new Error(data.error || "Не удалось найти город");
        setCdekCities(data.cities ?? []);
      } catch (error) {
        if ((error as Error).name !== "AbortError") {
          setCdekError(error instanceof Error ? error.message : "Не удалось найти город");
        }
      } finally {
        setCdekLoading(false);
      }
    }, 350);
    return () => {
      controller.abort();
      window.clearTimeout(timer);
    };
  }, [delivery, cdekCityQuery, cdekCity]);

  const deliveryOptions = useMemo(
    () => baseDeliveryOptions.filter((item) => item.id !== "cdek" || cdekAvailable),
    [cdekAvailable],
  );
  useEffect(() => {
    if (!checkoutOpen || orderNumber) return;
    saveCheckoutDraft({ owner: draftOwner, step, profile: checkoutProfile, delivery, deliveryConfirmed, paymentMethod, city: cdekCity, officeCode: cdekOfficeCode || (cdekLoading ? pendingRestoreOffice.current : ""), repack: cdekRepack });
  }, [draftOwner, checkoutOpen, orderNumber, step, checkoutProfile, delivery, deliveryConfirmed, paymentMethod, cdekCity, cdekOfficeCode, cdekRepack, draft, cdekLoading]);

  const originalSubtotal = cartLines.reduce((sum, item) => sum + item.price * item.quantity, 0);
  const subtotal = cartLines.reduce((sum, item) => sum + discountedPrice(item.price, discountBps) * item.quantity, 0);
  const discountAmount = Math.round((originalSubtotal - subtotal) * 100) / 100;
  const hasPreorder = cartLines.some((item) => item.stock !== undefined && item.stock < item.quantity);
  const availableDelivery = deliveryOptions;
  const deliveryOption = deliveryOptions.find((item) => item.id === delivery) ?? deliveryOptions[0];
  const cdekQuote =
    cdekQuotes.find((item) => item.tariffCode === cdekTariffCode) ?? cdekQuotes[0] ?? null;
  const cdekFeePending = delivery === "cdek" && (!cdekQuote || cdekRepack);
  const deliveryFeePending = cdekFeePending;
  const deliveryFee =
    delivery === "cdek"
      ? cdekFeePending
        ? 0
        : (cdekQuote?.price ?? 0)
      : (deliveryOption?.fee ?? 0);
  const officeSearch = cdekOfficeQuery.trim().toLowerCase();
  const cdekOfficeMatches = (
    officeSearch
      ? cdekOffices.filter((office) =>
          `${office.location.address} ${office.name}`.toLowerCase().includes(officeSearch),
        )
      : cdekOffices
  ).slice(0, 12);
  const selectedOffice = cdekOffices.find((office) => office.code === cdekOfficeCode) ?? null;
  const total = subtotal + deliveryFee;

  const itemsKey = JSON.stringify(cartLines.map(line => ({ id: line.id, quantity: line.quantity })));
  const chooseCdekCity = useCallback(async (city: CdekCity, restoreOffice = "") => {
    const request = ++quoteRequest.current;
    setCdekCity(city);
    setCdekCityQuery(city.city);
    setCdekCities([]);
    setCdekOffices([]);
    setCdekOfficeCode("");
    setCdekOfficeQuery("");
    setCdekQuotes([]);
    setCdekTariffCode(0);
    setCdekLoading(true);
    setCdekError("");
    try {
      const [officesResponse, quoteResponse] = await Promise.all([
        fetch(`/api/v1/delivery/cdek?action=offices&cityCode=${city.code}`),
        fetch("/api/v1/delivery/cdek", {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({
            cityCode: city.code,
            items: JSON.parse(itemsKey) as Array<{ id: string; quantity: number }>,
          }),
        }).catch(() => null),
      ]);
      const officesData = (await officesResponse.json()) as { offices?: CdekOffice[]; error?: string };
      const quoteData = (await quoteResponse?.json().catch(() => ({})) ?? {}) as { quotes?: CdekQuote[]; error?: string };
      if (request !== quoteRequest.current) return;
      if (!officesResponse.ok) {
        throw new Error(officesData.error || "Не удалось загрузить пункты выдачи");
      }
      if (!officesData.offices?.length) {
        throw new Error("В этом городе нет доступных пунктов выдачи");
      }
      setCdekOffices(officesData.offices);
      const restored = officesData.offices.find(office => office.code === restoreOffice);
      if (restored) { setCdekOfficeCode(restored.code); setCdekOfficeQuery(restored.location.address); }
      if (quoteData.quotes?.length) {
        const sorted = [...quoteData.quotes].sort((left, right) => left.price - right.price);
        // Same destination and transit window: the buyer only needs the cheapest.
        const choices = sorted.filter((quote, index) => !sorted.slice(0, index).some(earlier => earlier.daysMax <= quote.daysMax && earlier.daysMin <= quote.daysMin));
        setCdekQuotes(choices);
        setCdekTariffCode(choices[0].tariffCode);
      } else {
        setCdekQuotes([]);
        setCdekTariffCode(0);
      }
    } catch (error) {
      if (request === quoteRequest.current) setCdekError(error instanceof Error ? error.message : "Не удалось рассчитать доставку");
    } finally {
      if (request === quoteRequest.current) setCdekLoading(false);
    }
  }, [itemsKey]);

  const restoredCity = useRef(false);
  useEffect(() => {
    if (!checkoutOpen || restoredCity.current || !draft?.city || !cartLines.length) return;
    restoredCity.current = true;
    lastItemsKey.current = itemsKey;
    void chooseCdekCity(draft.city, draft.officeCode);
  }, [draft, cartLines.length, chooseCdekCity, itemsKey, checkoutOpen]);

  // Requote after a quantity/variant change; an old tariff must not be reused.
  const lastItemsKey = useRef(itemsKey);
  useEffect(() => {
    if (lastItemsKey.current === itemsKey) return;
    lastItemsKey.current = itemsKey;
    if (checkoutOpen && delivery === "cdek" && cdekCity && cartLines.length) void chooseCdekCity(cdekCity, cdekOfficeCode);
  }, [itemsKey, delivery, cdekCity, cdekOfficeCode, chooseCdekCity, cartLines.length, checkoutOpen]);

  async function submitOrder(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (submitLock.current) return;
    setOrderError("");
    setSubmitting(true);
    const form = new FormData(event.currentTarget);
    const phoneInput = event.currentTarget.elements.namedItem("phone") as HTMLInputElement;
    const phone = normalizeRussianPhone(String(form.get("phone") ?? ""));
    if (!phone) {
      phoneInput.setCustomValidity(
        "Введите российский номер: 9151234567, 79151234567 или 89151234567",
      );
      phoneInput.reportValidity();
      setSubmitting(false);
      return;
    }
    phoneInput.setCustomValidity("");
    phoneInput.value = phone;
    submitLock.current = true;
    const payload = {
      customer: {
        name: String(form.get("name") ?? ""),
        phone,
        email: String(form.get("email") ?? ""),
        address: String(form.get("address") ?? ""),
        comment: String(form.get("comment") ?? ""),
      },
      delivery,
      cdek:
        delivery === "cdek"
          ? {
              cityCode: cdekCity?.code,
              cityName: cdekCity?.city,
              officeCode: cdekOfficeCode,
              tariffCode: cdekRepack ? 0 : (cdekQuote?.tariffCode ?? 0),
              repack: cdekRepack,
            }
          : undefined,
      items: cartLines.map((item) => ({ id: item.id, quantity: item.quantity })),
      consent: form.get("consent") === "on",
	  paymentMethod: hasPreorder || deliveryFeePending ? "" : paymentMethod,
	  attribution: getAttribution(),
    };

    const needsManagerConfirmation = cartLines.some((item) => item.available === false || (item.stock !== undefined && item.stock < item.quantity));
    try {
      const response = await fetch("/api/v1/orders", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(payload),
      });
      const data = (await response.json()) as { orderNumber?: string; paymentStatus?: string; hasPreorder?: boolean; error?: string };
      if (!response.ok || !data.orderNumber) {
        throw new Error(data.error || "Не удалось оформить заказ");
      }
	  track("add_shipping_info", { value: subtotal, quantity: cartCount, properties: { delivery } });
	  if (!hasPreorder) track("add_payment_info", { value: subtotal, quantity: cartCount, properties: { paymentMethod } });
      setOrderConfirmationPending(needsManagerConfirmation || deliveryFeePending || data.hasPreorder === true || data.paymentStatus === "manager_confirmation");
      clearCheckoutDraft();
      setOrderNumber(data.orderNumber);
      setCart({});
      window.scrollTo({ top: 0, behavior: "auto" });
      if (!hasPreorder && paymentMethod === "online" && data.paymentStatus === "pending" && !data.hasPreorder && !deliveryFeePending) {
        try {
          const payment = await fetch(`/api/v1/payments/orders/${data.orderNumber}`, {
            method: "POST",
            credentials: "same-origin",
          });
          const result = (await payment.json()) as { confirmationUrl?: string; error?: string };
          if (!payment.ok) throw new Error(result.error || "Не удалось начать оплату");
          if (result.confirmationUrl) {
			track("payment_redirect", { value: subtotal, quantity: cartCount, properties: { orderNumber: data.orderNumber } });
            window.location.assign(result.confirmationUrl);
            return;
          }
        } catch {
          setNotice("Заказ оформлен. Оплатить можно из личного кабинета");
        }
      }
    } catch (error) {
	  track("checkout_error", { value: total, quantity: cartCount, properties: { message: error instanceof Error ? error.message.slice(0, 200) : "unknown", delivery, paymentMethod } });
      setOrderError(error instanceof Error ? error.message : "Не удалось оформить заказ");
    } finally {
      submitLock.current = false;
      setSubmitting(false);
    }
  }

  function beginCheckout() {
    setCheckoutOpen(true);
    setOrderNumber("");
    setOrderConfirmationPending(false);
  }

  return {
    checkoutOpen,
    setCheckoutOpen,
    beginCheckout,
    setCheckoutProfile,
    applyCustomerProfile,
    availableDelivery,
    panelProps: {
      cartLines, step, setStep, deliveryConfirmed, setDeliveryConfirmed, orderError,
      retryCdek: () => { if (cdekCity) void chooseCdekCity(cdekCity, cdekOfficeCode); },
      retryPaymentMethods: () => setMethodsRevision(current => current + 1),
      checkoutOpen,
      setCheckoutOpen,
      orderNumber,
      orderConfirmationPending,
      submitOrder,
      checkoutProfile,
      setCheckoutProfile,
      availableDelivery,
      delivery,
      setDelivery,
      cdekQuote,
      cdekCityQuery,
      setCdekCityQuery,
      setCdekCity: (value: SetStateAction<CdekCity | null>) => { quoteRequest.current++; setCdekLoading(false); setCdekCity(value); },
      setCdekCities,
      setCdekOffices,
      cdekOfficeCode,
      setCdekOfficeCode,
      cdekOfficeQuery,
      setCdekOfficeQuery,
      setCdekQuotes,
      setCdekTariffCode,
      cdekCities,
      chooseCdekCity,
      cdekLoading,
      cdekError,
      cdekOffices,
      setCdekOfficeListOpen,
      cdekOfficeListOpen,
      cdekOfficeMatches,
      selectedOffice,
      cdekFeePending,
      cdekRepack,
      setCdekRepack,
      cartCount,
      hasPreorder,
      cdekQuotes,
      paymentMethods,
      paymentMethod,
      setPaymentMethod,
      subtotal,
      originalSubtotal,
      discountAmount,
      discountBps,
      deliveryFee,
      total,
      submitting,
      deliveryFeePending,
    },
  };
}
