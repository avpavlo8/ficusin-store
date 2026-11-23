import { useEffect, useRef, useState } from "react";
import content from "./instructions/content.json";
import { AdminIcon } from "./AdminWorkspace";
import "./styles/admin-instructions.css";

type Guide = (typeof content.guides)[number];
const imagePath = (guide: Guide, step: number) =>
  `/assets/instructions/v1/${guide.id}/${step + 1}.png`;
const stepCount = (count: number) => `${count} ${count === 3 || count === 4 ? "шага" : "шагов"}`;
const normalized = (value: string) => value.toLocaleLowerCase("ru").replaceAll("ё", "е").trim();
const serviceSections = content.services.filter((item) => item.id !== "general");
const logoPaths: Record<string, string> = {
  saby: "/assets/instructions/logos/saby.svg",
  wb: "/assets/instructions/logos/wb.svg",
  ozon: "/assets/instructions/logos/ozon.svg",
  cdek: "/assets/instructions/logos/cdek.svg",
  site: "/assets/brand/graphite-v1/wordmark-graphite.svg",
  flowwow: "/assets/instructions/logos/flowwow.svg",
  avito: "/assets/instructions/logos/avito.svg",
  fivepost: "/assets/instructions/logos/fivepost.svg",
  cash: "/assets/instructions/logos/cash.svg",
};
const guideCount = (count: number) => {
  const suffix =
    count % 100 >= 11 && count % 100 <= 14
      ? "инструкций"
      : count % 10 === 1
        ? "инструкция"
        : [2, 3, 4].includes(count % 10)
          ? "инструкции"
          : "инструкций";
  return `${count} ${suffix}`;
};
function ServiceLogo({ id }: { id: string }) {
  return (
    <img
      className="instruction-service-logo"
      src={logoPaths[id]}
      width="160"
      height="48"
      alt=""
      draggable={false}
    />
  );
}
function readLocation() {
  const params = new URLSearchParams(window.location.search);
  return {
    guide: params.get("guide") || "",
    service: params.get("service") || "all",
    query: params.get("q") || "",
  };
}

function InstructionImage({ guide, index }: { guide: Guide; index: number }) {
  const [failed, setFailed] = useState(false);
  const dialog = useRef<HTMLDialogElement>(null);
  const step = guide.steps[index];
  const src = imagePath(guide, index);
  return (
    <figure className="instruction-step" id={`instruction-step-${index + 1}`}>
      <div className="instruction-step-heading">
        <h3>
          <span>{index + 1}</span>
          {step.title}
        </h3>
        <a href={src} download={`${guide.id}-${index + 1}.png`}>
          Скачать PNG
        </a>
      </div>
      {failed ? (
        <div className="instruction-image-error" role="status">
          <p>Не удалось загрузить картинку. Текст шага доступен ниже.</p>
          <button className="ws-button" type="button" onClick={() => setFailed(false)}>
            Загрузить снова
          </button>
        </div>
      ) : (
        <button
          className="instruction-image-button"
          type="button"
          onClick={() => dialog.current?.showModal()}
          aria-label={`Увеличить шаг ${index + 1}: ${step.title}`}
        >
          <img
            src={src}
            alt={`${step.title}. ${step.text}`}
            width="1200"
            height="760"
            loading="eager"
            onError={() => setFailed(true)}
          />
          <span>Увеличить изображение</span>
        </button>
      )}
      <figcaption>
        <p>{step.text}</p>
        <p>
          <strong>Проверка:</strong> {step.result}
        </p>
      </figcaption>
      <dialog
        ref={dialog}
        className="instruction-zoom"
        aria-label={`Шаг ${index + 1}: ${step.title}`}
        onClick={(event) => {
          if (event.target === event.currentTarget) event.currentTarget.close();
        }}
      >
        <div className="instruction-zoom-toolbar">
          <strong>
            Шаг {index + 1} · {step.title}
          </strong>
          <button className="ws-button" type="button" onClick={() => dialog.current?.close()}>
            Закрыть изображение
          </button>
        </div>
        <div className="instruction-zoom-scroll">
          <img src={src} alt={`${step.title}. ${step.text}`} width="1200" height="760" />
        </div>
      </dialog>
    </figure>
  );
}

export default function AdminInstructions() {
  const [location, setLocation] = useState(readLocation);
  const heading = useRef<HTMLHeadingElement>(null);
  const [printing, setPrinting] = useState(false);
  const [printError, setPrintError] = useState("");
  const guide = content.guides.find((item) => item.id === location.guide);
  const service = content.services.find((item) => item.id === guide?.service);
  const navigate = (next: typeof location) => {
    const url = new URL(window.location.href);
    for (const key of ["guide", "service", "query"] as const) {
      const parameter = key === "query" ? "q" : key;
      if (next[key] && next[key] !== "all") url.searchParams.set(parameter, next[key]);
      else url.searchParams.delete(parameter);
    }
    url.hash = "";
    window.history.pushState(null, "", url);
    setLocation(next);
    setPrintError("");
    heading.current?.focus();
    document.getElementById("workspace-content")?.scrollTo(0, 0);
    window.scrollTo(0, 0);
  };
  useEffect(() => {
    const sync = () => {
      setLocation(readLocation());
      setPrintError("");
    };
    window.addEventListener("popstate", sync);
    return () => window.removeEventListener("popstate", sync);
  }, []);
  useEffect(() => {
    heading.current?.focus();
  }, [location.guide]);
  const printGuide = async () => {
    if (!guide) return;
    setPrinting(true);
    setPrintError("");
    try {
      await Promise.all(
        guide.steps.map(async (_, index) => {
          const image = new Image();
          image.src = imagePath(guide, index);
          await image.decode();
        }),
      );
      if (new URL(window.location.href).searchParams.get("guide") === guide.id) window.print();
    } catch {
      setPrintError(
        "Не удалось подготовить все картинки к печати. Проверьте соединение и попробуйте снова.",
      );
    } finally {
      setPrinting(false);
    }
  };
  const query = normalized(location.query);
  const relevance = (item: Guide) =>
    query
      .split(/\s+/)
      .filter(Boolean)
      .reduce(
        (score, word) =>
          score +
          (normalized(item.title).includes(word) ? 4 : 0) +
          (normalized(item.summary).includes(word) ? 2 : 0),
        0,
      );
  const matches = content.guides
    .filter((item) => {
      const channel = content.services.find((entry) => entry.id === item.service);
      const text = normalized(
        [
          item.title,
          item.summary,
          channel?.label,
          channel?.keywords,
          ...item.steps.map((step) => `${step.title} ${step.text}`),
        ].join(" "),
      );
      return query.split(/\s+/).every((word) => text.includes(word));
    })
    .sort((left, right) => relevance(right) - relevance(left));
  const selectedService = serviceSections.find((item) => item.id === location.service);
  const generalGuides = matches.filter((item) => item.service === "general");
  const serviceGuides = matches.filter((item) => item.service === selectedService?.id);
  const renderGuides = (items: Guide[], search = false) => (
    <div className="instruction-list" data-search={search || undefined}>
      {items.map((entry) => (
        <a
          key={entry.id}
          className="instruction-guide-link"
          data-service={entry.service}
          href={`/admin?section=instructions&guide=${entry.id}${location.service !== "all" ? `&service=${location.service}` : ""}${location.query ? `&q=${encodeURIComponent(location.query)}` : ""}`}
          onClick={(event) => {
            if (event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return;
            event.preventDefault();
            navigate({ ...location, guide: entry.id });
          }}
        >
          <div>
            {search && (
              <span className="instruction-service-badge">
                {content.services.find((item) => item.id === entry.service)?.label}
              </span>
            )}
            <strong>{entry.title}</strong>
            <p>{entry.summary}</p>
            <small>
              {stepCount(entry.steps.length)} ·{" "}
              {entry.steps.some((step) => "screen" in step)
                ? "Скриншоты и схемы"
                : "Схема действий"}
            </small>
          </div>
          <AdminIcon name="arrow" />
        </a>
      ))}
    </div>
  );

  if (guide)
    return (
      <section className="instructions instruction-reader" aria-label="Инструкция">
        <a
          className="instruction-back"
          href={`/admin?section=instructions${location.service !== "all" ? `&service=${location.service}` : ""}${location.query ? `&q=${encodeURIComponent(location.query)}` : ""}`}
          onClick={(event) => {
            event.preventDefault();
            navigate({ ...location, guide: "" });
          }}
        >
          Все инструкции / {service?.label}
        </a>
        <header className="instruction-reader-heading">
          <div>
            <h2 ref={heading} tabIndex={-1}>
              {guide.title}
            </h2>
            <p>{guide.summary}</p>
          </div>
          <button
            className="ws-button"
            type="button"
            disabled={printing}
            onClick={() => void printGuide()}
          >
            {printing ? "Готовим картинки…" : "Печать / PDF"}
          </button>
        </header>
        {printError && (
          <p className="instruction-note" role="alert">
            {printError}
          </p>
        )}
        {guide.note && <p className="instruction-note">{guide.note}</p>}
        <div className="instruction-reading-layout">
          <details className="instruction-mobile-contents">
            <summary>Шаги инструкции · {stepCount(guide.steps.length)}</summary>
            <nav aria-label="Шаги инструкции на телефоне">
              {guide.steps.map((step, index) => (
                <a
                  key={step.title}
                  href={`#instruction-step-${index + 1}`}
                  onClick={(event) =>
                    event.currentTarget.closest("details")?.removeAttribute("open")
                  }
                >
                  {index + 1}. {step.title}
                </a>
              ))}
            </nav>
          </details>
          <nav className="instruction-contents" aria-label="Шаги инструкции">
            <strong>{stepCount(guide.steps.length)}</strong>
            {guide.steps.map((step, index) => (
              <a key={step.title} href={`#instruction-step-${index + 1}`}>
                <span>{index + 1}</span>
                {step.title}
              </a>
            ))}
          </nav>
          <div className="instruction-pages">
            {guide.steps.map((step, index) => (
              <InstructionImage key={`${guide.id}-${step.title}`} guide={guide} index={index} />
            ))}
            {guide.sources.length > 0 && (
              <footer className="instruction-sources">
                <strong>Справка сервиса</strong>
                {guide.sources.map((source) => (
                  <a key={source.url} href={source.url} target="_blank" rel="noreferrer">
                    {source.label}
                  </a>
                ))}
              </footer>
            )}
          </div>
        </div>
      </section>
    );

  return (
    <section className="instructions" aria-label="Инструкции">
      <header className="instruction-index-heading">
        <div>
          <h2 ref={heading} tabIndex={-1}>
            Инструкции
          </h2>
          <p>Выберите сервис или найдите нужное действие.</p>
        </div>
        <span>
          {guideCount(content.guides.length)} · {serviceSections.length} разделов
        </span>
      </header>
      {location.guide && (
        <p className="instruction-note" role="status">
          Инструкция не найдена. Выберите её из списка.
        </p>
      )}
      <div className="instruction-search">
        <AdminIcon name="search" />
        <label className="instruction-search-label" htmlFor="instruction-search">
          Поиск по инструкциям
        </label>
        <input
          id="instruction-search"
          type="search"
          placeholder="Например, возврат или этикетки"
          value={location.query}
          onChange={(event) => {
            const query = event.target.value;
            setLocation((previous) => ({ ...previous, query }));
            const url = new URL(window.location.href);
            if (query) url.searchParams.set("q", query);
            else url.searchParams.delete("q");
            window.history.replaceState(null, "", url);
          }}
        />
      </div>
      {query ? (
        <section className="instruction-search-results" aria-label="Результаты поиска инструкций">
          <header className="instruction-results-heading">
            <h3>Результаты поиска</h3>
            <p role="status">Найдено: {guideCount(matches.length)}</p>
          </header>
          {matches.length ? (
            renderGuides(matches, true)
          ) : (
            <div className="instruction-empty">
              <h3>По этому запросу ничего не найдено</h3>
              <p>Попробуйте название действия: «продажа», «возврат» или «доставка».</p>
              <button
                className="ws-button"
                type="button"
                onClick={() => navigate({ guide: "", service: "all", query: "" })}
              >
                Сбросить поиск и фильтр
              </button>
            </div>
          )}
        </section>
      ) : (
        <>
          {selectedService ? (
            <section
              className="instruction-group instruction-selected-service"
              aria-label={selectedService.label}
            >
              <a
                className="instruction-back"
                href="/admin?section=instructions"
                onClick={(event) => {
                  if (event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return;
                  event.preventDefault();
                  navigate({ guide: "", service: "all", query: "" });
                }}
              >
                Все сервисы
              </a>
              <header className="instruction-selected-heading" data-service={selectedService.id}>
                <ServiceLogo id={selectedService.id} />
                <div>
                  <h3>{selectedService.label}</h3>
                  <p>{selectedService.description}</p>
                </div>
              </header>
              {serviceGuides.length ? (
                renderGuides(serviceGuides)
              ) : (
                <div className="instruction-empty">
                  <h3>Инструкции по {selectedService.label} ещё готовятся</h3>
                  <p>Пока можно воспользоваться общими памятками ниже.</p>
                </div>
              )}
            </section>
          ) : (
            <section className="instruction-service-directory" aria-label="Инструкции по сервисам">
              <header>
                <h3>Инструкции по сервисам</h3>
                <p>Отдельный раздел для каждого рабочего инструмента.</p>
              </header>
              <nav className="instruction-service-grid" aria-label="Сервисы инструкций">
                {serviceSections.map((item) => {
                  const count = content.guides.filter((entry) => entry.service === item.id).length;
                  return (
                    <a
                      key={item.id}
                      className="instruction-service-card"
                      data-service={item.id}
                      aria-label={item.label}
                      href={`/admin?section=instructions&service=${item.id}`}
                      onClick={(event) => {
                        if (event.metaKey || event.ctrlKey || event.shiftKey || event.altKey)
                          return;
                        event.preventDefault();
                        navigate({ guide: "", service: item.id, query: "" });
                      }}
                    >
                      <ServiceLogo id={item.id} />
                      <div>
                        <strong>{item.label}</strong>
                        <small>{count ? guideCount(count) : "Инструкции готовятся"}</small>
                      </div>
                      <AdminIcon name="arrow" />
                    </a>
                  );
                })}
              </nav>
            </section>
          )}
          <section className="instruction-group instruction-general" aria-label="Общие инструкции">
            <header>
              <h3>Общие инструкции</h3>
              <p>Памятки для ежедневной работы, независимо от сервиса.</p>
            </header>
            {renderGuides(generalGuides)}
          </section>
        </>
      )}
    </section>
  );
}
