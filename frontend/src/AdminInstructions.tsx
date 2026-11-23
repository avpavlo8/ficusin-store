import { useEffect, useRef, useState, type MouseEvent } from "react";
import content from "./instructions/content.json";
import { AdminIcon } from "./AdminWorkspace";
import { APIError, apiJSON } from "./lib/api";
import "./styles/admin-instructions.css";

type Guide = (typeof content.guides)[number];
type Step = Guide["steps"][number];
type BoxLayout = { title: string; imageFile: string; width: number; height: number };
const hasImage = (step: Step) => step.imageStyle !== "text";
const assetPath = (guide: Guide, file: string) => `/assets/instructions/v1/${guide.id}/${file}`;
const imageFile = (step: Step, index: number) => step.imageFile ?? `${index + 1}.png`;
const stepCount = (count: number) =>
  `${count} ${count % 100 >= 11 && count % 100 <= 14 ? "шагов" : count % 10 === 1 ? "шаг" : [2, 3, 4].includes(count % 10) ? "шага" : "шагов"}`;
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

type CdekCredentials = { login: string; password: string };

function CdekCredentialsHelp() {
  const dialog = useRef<HTMLDialogElement>(null);
  const request = useRef<AbortController | null>(null);
  const [credentials, setCredentials] = useState<CdekCredentials | null>(null);
  const [error, setError] = useState("");

  async function loadCredentials() {
    request.current?.abort();
    const controller = new AbortController();
    request.current = controller;
    setError("");
    try {
      const data = await apiJSON<CdekCredentials>("/api/v1/admin/instructions/cdek-credentials", {
        cache: "no-store",
        signal: controller.signal,
      });
      if (!controller.signal.aborted) setCredentials(data);
    } catch (error) {
      if (controller.signal.aborted) return;
      setError(
        error instanceof APIError ? error.message : "Не удалось загрузить данные для входа.",
      );
    }
  }

  return (
    <>
      <button
        className="ws-button instruction-credentials-trigger"
        type="button"
        aria-label="Показать логин и пароль СДЭК"
        aria-haspopup="dialog"
        onClick={() => {
          dialog.current?.showModal();
          void loadCredentials();
        }}
      >
        ?
      </button>
      <dialog
        ref={dialog}
        className="instruction-credentials-dialog"
        aria-labelledby="instruction-credentials-title"
        onClose={() => {
          request.current?.abort();
          setCredentials(null);
          setError("");
        }}
      >
        <h4 id="instruction-credentials-title">Данные для входа в СДЭК</h4>
        {credentials ? (
          <dl>
            <dt>Логин</dt>
            <dd>{credentials.login}</dd>
            <dt>Пароль</dt>
            <dd>{credentials.password}</dd>
          </dl>
        ) : error ? (
          <p role="alert">{error}</p>
        ) : (
          <p role="status">Загружаем данные…</p>
        )}
        <div className="instruction-credentials-actions">
          {error && (
            <button type="button" className="ws-button" onClick={() => void loadCredentials()}>
              Повторить
            </button>
          )}
          <button type="button" className="ws-button" onClick={() => dialog.current?.close()}>
            Закрыть
          </button>
        </div>
      </dialog>
    </>
  );
}

function StepImage({
  src,
  label,
  alt,
  loading = "eager",
  width = 1200,
  height = 760,
}: {
  src: string;
  label: string;
  alt: string;
  loading?: "eager" | "lazy";
  width?: number;
  height?: number;
}) {
  const [failed, setFailed] = useState(false);
  const dialog = useRef<HTMLDialogElement>(null);
  return (
    <>
      {failed ? (
        <div className="instruction-image-error" role="status">
          <p>Не удалось загрузить картинку.</p>
          <button className="ws-button" type="button" onClick={() => setFailed(false)}>
            Загрузить снова
          </button>
        </div>
      ) : (
        <button
          className="instruction-image-button"
          type="button"
          onClick={() => dialog.current?.showModal()}
          aria-label={`Увеличить ${label}`}
        >
          <img
            src={src}
            alt={alt}
            width={width}
            height={height}
            loading={loading}
            onError={() => setFailed(true)}
          />
          <span>Увеличить изображение</span>
        </button>
      )}
      <dialog
        ref={dialog}
        className="instruction-zoom"
        aria-label={label}
        onClick={(event) => {
          if (event.target === event.currentTarget) event.currentTarget.close();
        }}
      >
        <div className="instruction-zoom-toolbar">
          <strong>{label}</strong>
          <button className="ws-button" type="button" onClick={() => dialog.current?.close()}>
            Закрыть изображение
          </button>
        </div>
        <div className="instruction-zoom-scroll">
          <img src={src} alt={alt} width={width} height={height} loading={loading} />
        </div>
      </dialog>
    </>
  );
}

function BoxLayouts({ guide, boxes }: { guide: Guide; boxes: BoxLayout[] }) {
  return (
    <section className="instruction-box-layouts" aria-labelledby="instruction-box-layouts-title">
      <h3 id="instruction-box-layouts-title">Выберите коробку</h3>
      {boxes.length === 0 && <p>Схемы раскроя ещё добавляются.</p>}
      {boxes.map((box) => {
        const src = assetPath(guide, box.imageFile);
        return (
          <details
            key={box.imageFile}
            className="instruction-box-layout"
            data-file={box.imageFile}
            name="instruction-box-size"
          >
            <summary>{box.title}</summary>
            <div className="instruction-box-layout-body">
              <div className="instruction-step-heading">
                <a href={src} download={box.imageFile}>
                  Скачать раскрой PNG
                </a>
              </div>
              <StepImage
                src={src}
                label={`раскрой для коробки ${box.title}`}
                alt={`Схема раскроя пенопласта для коробки ${box.title}`}
                loading="lazy"
                width={box.width}
                height={box.height}
              />
            </div>
          </details>
        );
      })}
    </section>
  );
}

function InstructionStep({ guide, index }: { guide: Guide; index: number }) {
  const step = guide.steps[index];
  const id = `instruction-step-${index + 1}`;
  const heading = (
    <h3>
      <span>{index + 1}</span>
      {step.title}
    </h3>
  );
  if (step.action) {
    return (
      <section className="instruction-step instruction-action-step" id={id}>
        <div className="instruction-step-heading">{heading}</div>
        <p>{step.text}</p>
        <div className="instruction-action-buttons">
          <a className="ws-button primary" href={step.action.url} target="_blank" rel="noreferrer">
            {step.action.label}
          </a>
          {guide.id === "cdek-create" && <CdekCredentialsHelp />}
        </div>
        <p className="instruction-action-result">{step.result}</p>
      </section>
    );
  }
  const { example } = step;
  const src = assetPath(guide, imageFile(step, index));
  return (
    <figure className="instruction-step" data-has-example={Boolean(example) || undefined} id={id}>
      <div className="instruction-step-heading">
        {heading}
        <a href={src} download={`${guide.id}-${index + 1}.png`}>
          Скачать PNG
        </a>
      </div>
      <div className="instruction-step-description">
        {step.text.split("\n\n").map((paragraph) => (
          <p key={paragraph}>{paragraph}</p>
        ))}
        {step.links?.map((link) => (
          <a key={link.url} href={link.url} target="_blank" rel="noreferrer">
            {link.label}
          </a>
        ))}
      </div>
      <StepImage src={src} label={`шаг ${index + 1}: ${step.title}`} alt={step.title} />
      {example && (
        <div className="instruction-step-example">
          <div className="instruction-step-heading">
            <h4>{example.title}</h4>
            <a href={assetPath(guide, example.imageFile)} download={example.imageFile}>
              Скачать PNG
            </a>
          </div>
          <StepImage
            src={assetPath(guide, example.imageFile)}
            label={`пример: ${example.title}`}
            alt={example.alt}
          />
        </div>
      )}
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
  const openDirectory = (event: MouseEvent<HTMLAnchorElement>, serviceId = "all") => {
    if (event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return;
    event.preventDefault();
    navigate({ guide: "", service: serviceId, query: "" });
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
      const files = guide.steps.flatMap((step, index) => {
        if (!hasImage(step)) return [];
        const files = [imageFile(step, index)];
        if (step.example) files.push(step.example.imageFile);
        return files;
      });
      if (guide.boxLayouts) {
        const selectedBox = document.querySelector<HTMLElement>(".instruction-box-layout[open]");
        if (selectedBox?.dataset.file) files.push(selectedBox.dataset.file);
      }
      await Promise.all(
        files.map(async (file) => {
          const image = new Image();
          image.src = assetPath(guide, file);
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
              {entry.boxLayouts
                ? `${entry.boxLayouts.length} карт раскроя`
                : `${stepCount(entry.steps.length)} · Скриншоты`}
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
        <nav className="instruction-reader-nav" aria-label="Навигация по инструкциям">
          {service && service.id !== "general" && (
            <a
              className="ws-button instruction-reader-service"
              href={`/admin?section=instructions&service=${service.id}`}
              onClick={(event) => openDirectory(event, service.id)}
            >
              <AdminIcon name="arrow" />
              Инструкции {service.id === "cdek" ? "СДЭК" : service.label}
            </a>
          )}
          <a
            className="ws-button"
            href="/admin?section=instructions"
            onClick={(event) => openDirectory(event)}
          >
            <AdminIcon name="dashboard" />
            На главную
          </a>
        </nav>
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
        <div className="instruction-linear">
          <div className="instruction-pages">
            {guide.boxLayouts && <BoxLayouts guide={guide} boxes={guide.boxLayouts} />}
            {guide.steps.map((step, index) => (
              <InstructionStep key={`${guide.id}-${step.title}`} guide={guide} index={index} />
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
          placeholder="Например, СДЭК или пенопласт"
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
              <p>Попробуйте «СДЭК» или «пенопласт».</p>
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
                className="instruction-service-back"
                href="/admin?section=instructions"
                onClick={(event) => openDirectory(event)}
              >
                <AdminIcon name="arrow" />
                <span>Все сервисы</span>
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
                  <p>Выберите «Все сервисы», чтобы вернуться к списку разделов.</p>
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
          {!selectedService && (
            <section
              className="instruction-group instruction-general"
              aria-label="Общие инструкции"
            >
              <header>
                <h3>Общие инструкции</h3>
                <p>Памятки для ежедневной работы, независимо от сервиса.</p>
              </header>
              {renderGuides(generalGuides)}
            </section>
          )}
        </>
      )}
    </section>
  );
}
