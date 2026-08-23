import type { Dispatch, FormEvent, SetStateAction } from "react";
import { useEffect, useMemo, useState } from "react";
import type { CartLine } from "./CartCheckout";
import { normalizeRussianPhone } from "./lib/phone";

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
export type AddressDeliveryQuote = {
  price: number;
  daysMin?: number;
  daysMax?: number;
  service?: string;
};

const baseDeliveryOptions = [
  { id: "pickup", title: "Самовывоз в Рязани", detail: "Бесплатно", fee: 0 },
  { id: "courier", title: "Курьер по Рязани", detail: "По тарифу Яндекс Доставки", fee: null },
  { id: "cdek", title: "СДЭК по России", detail: "Рассчитаем по адресу", fee: null },
  { id: "post", title: "Почта России", detail: "По тарифу вашего договора с Почтой России", fee: null },
] as const;

type UseCheckoutArgs = {
  cartLines: CartLine[];
  cartCount: number;
  setCart: Dispatch<SetStateAction<Cart>>;
  setNotice: Dispatch<SetStateAction<string>>;
  initialOpen?: boolean;
};

export function useCheckout({ cartLines, cartCount, setCart, setNotice, initialOpen = false }: UseCheckoutArgs) {
  const [checkoutOpen, setCheckoutOpen] = useState(initialOpen);
  const [delivery, setDelivery] = useState("pickup");
  const [submitting, setSubmitting] = useState(false);
  const [orderNumber, setOrderNumber] = useState("");
  const [orderConfirmationPending, setOrderConfirmationPending] = useState(false);
  const [cdekCityQuery, setCdekCityQuery] = useState("");
  const [cdekCities, setCdekCities] = useState<CdekCity[]>([]);
  const [cdekCity, setCdekCity] = useState<CdekCity | null>(null);
  const [cdekOffices, setCdekOffices] = useState<CdekOffice[]>([]);
  const [cdekOfficeCode, setCdekOfficeCode] = useState("");
  const [cdekOfficeQuery, setCdekOfficeQuery] = useState("");
  const [cdekOfficeListOpen, setCdekOfficeListOpen] = useState(false);
  const [cdekQuotes, setCdekQuotes] = useState<CdekQuote[]>([]);
  const [cdekTariffCode, setCdekTariffCode] = useState(0);
  const [cdekRepack, setCdekRepack] = useState(false);
  const [paymentMethods, setPaymentMethods] = useState<PaymentMethod[]>([]);
  const [paymentMethod, setPaymentMethod] = useState("online");
  const [cdekLoading, setCdekLoading] = useState(false);
  const [cdekError, setCdekError] = useState("");
  const [cdekAvailable, setCdekAvailable] = useState(true);
  const [providerAvailability, setProviderAvailability] = useState({ courier: false, post: false });
  const [deliveryQuote, setDeliveryQuote] = useState<AddressDeliveryQuote | null>(null);
  const [deliveryQuoteLoading, setDeliveryQuoteLoading] = useState(false);
  const [deliveryQuotePending, setDeliveryQuotePending] = useState(false);
  const [deliveryQuoteError, setDeliveryQuoteError] = useState("");
  const [checkoutProfile, setCheckoutProfile] = useState<CheckoutProfile>({
    name: "",
    phone: "",
    email: "",
    address: "",
  });

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
  }, [delivery]);

  useEffect(() => {
    fetch("/api/v1/delivery/cdek?action=status", { cache: "no-store" })
      .then((response) => response.json())
      .then((data: { available?: boolean }) => setCdekAvailable(Boolean(data.available)))
      .catch(() => setCdekAvailable(false));
  }, []);

  useEffect(() => {
    fetch("/api/v1/delivery/providers", { cache: "no-store" })
      .then((response) => response.json())
      .then((data: { courier?: boolean; post?: boolean }) =>
        setProviderAvailability({ courier: Boolean(data.courier), post: Boolean(data.post) }),
      )
      .catch(() => setProviderAvailability({ courier: false, post: false }));
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

  const cartFingerprint = useMemo(
    () => cartLines.map((line) => `${line.id}:${line.quantity}`).sort().join("|"),
    [cartLines],
  );
  useEffect(() => {
    setDeliveryQuote(null);
    setDeliveryQuotePending(false);
    setDeliveryQuoteError("");
  }, [delivery, checkoutProfile.address, cartFingerprint]);

  const deliveryOptions = useMemo(
    () =>
      baseDeliveryOptions.filter((item) => {
        if (item.id === "cdek") return cdekAvailable;
        if (item.id === "courier") return providerAvailability.courier;
        if (item.id === "post") return providerAvailability.post;
        return true;
      }),
    [cdekAvailable, providerAvailability],
  );
  const subtotal = cartLines.reduce((sum, item) => sum + item.price * item.quantity, 0);
  const availableDelivery = deliveryOptions;
  const deliveryOption = deliveryOptions.find((item) => item.id === delivery) ?? deliveryOptions[0];
  const cdekQuote =
    cdekQuotes.find((item) => item.tariffCode === cdekTariffCode) ?? cdekQuotes[0] ?? null;
  const cdekFeePending = delivery === "cdek" && (!cdekQuote || cdekRepack);
  const addressDelivery = delivery === "courier" || delivery === "post";
  const addressDeliveryNeedsQuote = addressDelivery && !deliveryQuote && !deliveryQuotePending;
  const deliveryFeePending = cdekFeePending || (addressDelivery && deliveryQuotePending);
  const deliveryFee =
    delivery === "cdek"
      ? cdekFeePending
        ? 0
        : (cdekQuote?.price ?? 0)
      : addressDelivery
        ? (deliveryQuote?.price ?? 0)
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

  async function chooseCdekCity(city: CdekCity) {
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
            items: cartLines.map((line) => ({ id: line.id, quantity: line.quantity })),
          }),
        }),
      ]);
      const officesData = (await officesResponse.json()) as { offices?: CdekOffice[]; error?: string };
      const quoteData = (await quoteResponse.json()) as { quotes?: CdekQuote[]; error?: string };
      if (!officesResponse.ok) {
        throw new Error(officesData.error || "Не удалось загрузить пункты выдачи");
      }
      if (!officesData.offices?.length) {
        throw new Error("В этом городе нет доступных пунктов выдачи");
      }
      setCdekOffices(officesData.offices);
      if (quoteData.quotes?.length) {
        setCdekQuotes(quoteData.quotes);
        setCdekTariffCode(quoteData.quotes[0].tariffCode);
      } else {
        setCdekQuotes([]);
        setCdekTariffCode(0);
      }
    } catch (error) {
      setCdekError(error instanceof Error ? error.message : "Не удалось рассчитать доставку");
    } finally {
      setCdekLoading(false);
    }
  }

  async function calculateAddressDelivery() {
    if (!addressDelivery) return;
    const address = checkoutProfile.address.trim();
    if (!address) {
      setDeliveryQuoteError("Укажите адрес доставки");
      return;
    }
    setDeliveryQuoteLoading(true);
    setDeliveryQuoteError("");
    setDeliveryQuote(null);
    setDeliveryQuotePending(false);
    try {
      const response = await fetch(`/api/v1/delivery/${delivery}`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          address,
          items: cartLines.map((line) => ({ id: line.id, quantity: line.quantity })),
        }),
      });
      const data = (await response.json()) as {
        quote?: AddressDeliveryQuote;
        pending?: boolean;
        message?: string;
        error?: string;
      };
      if (!response.ok) throw new Error(data.error || "Не удалось рассчитать доставку");
      if (data.quote?.price && data.quote.price > 0) {
        setDeliveryQuote(data.quote);
        return;
      }
      if (data.pending) {
        setDeliveryQuotePending(true);
        setDeliveryQuoteError(data.message || "Стоимость уточнит менеджер");
        return;
      }
      throw new Error("Перевозчик не вернул стоимость доставки");
    } catch (error) {
      setDeliveryQuoteError(error instanceof Error ? error.message : "Не удалось рассчитать доставку");
    } finally {
      setDeliveryQuoteLoading(false);
    }
  }

  async function submitOrder(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (addressDeliveryNeedsQuote) {
      setNotice("Сначала рассчитайте стоимость доставки");
      return;
    }
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
      paymentMethod,
    };

    const needsManagerConfirmation = deliveryFeePending;
    try {
      const response = await fetch("/api/v1/orders", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(payload),
      });
      const data = (await response.json()) as { orderNumber?: string; error?: string };
      if (!response.ok || !data.orderNumber) {
        throw new Error(data.error || "Не удалось оформить заказ");
      }
      setOrderConfirmationPending(needsManagerConfirmation);
      setOrderNumber(data.orderNumber);
      setCart({});
      window.scrollTo({ top: 0, behavior: "auto" });
      if (paymentMethod === "online" && !deliveryFeePending) {
        try {
          const payment = await fetch(`/api/v1/payments/orders/${data.orderNumber}`, {
            method: "POST",
            credentials: "same-origin",
          });
          const result = (await payment.json()) as { confirmationUrl?: string };
          if (result.confirmationUrl) {
            window.location.assign(result.confirmationUrl);
            return;
          }
        } catch {
          setNotice("Заказ оформлен. Оплатить можно из личного кабинета");
        }
      }
    } catch (error) {
      setNotice(error instanceof Error ? error.message : "Не удалось оформить заказ");
    } finally {
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
    availableDelivery,
    panelProps: {
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
      setCdekCity,
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
      cdekQuotes,
      paymentMethods,
      paymentMethod,
      setPaymentMethod,
      subtotal,
      deliveryFee,
      total,
      submitting,
      deliveryQuote,
      deliveryQuoteLoading,
      deliveryQuotePending,
      deliveryQuoteError,
      deliveryFeePending,
      addressDeliveryNeedsQuote,
      calculateAddressDelivery,
    },
  };
}
