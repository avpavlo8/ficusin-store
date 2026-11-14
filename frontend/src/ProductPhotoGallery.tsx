import { useEffect, useRef, useState, type DragEvent, type PointerEvent } from "react";
import type { DraftMedia } from "./productCreationDraft";
import { readDraftMedia } from "./productCreationDraft";
import { moveGalleryPhoto } from "./galleryOrder";

function Preview({ item }: { item: DraftMedia }) {
  const [url, setURL] = useState("");
  const [failed, setFailed] = useState(false);
  const [failedSource, setFailedSource] = useState("");
  useEffect(() => {
    if (item.source) return;
    let active = true;
    let objectURL = "";
    readDraftMedia(item.id)
      .then((blob) => {
        if (!active) return;
        if (!blob) {
          setFailed(true);
          return;
        }
        objectURL = URL.createObjectURL(blob);
        setURL(objectURL);
      })
      .catch(() => {
        if (active) setFailed(true);
      });
    return () => {
      active = false;
      if (objectURL) URL.revokeObjectURL(objectURL);
    };
  }, [item.id, item.source]);
  const src = item.source || url;
  const unavailable = (!src && failed) || Boolean(src && failedSource === src);
  return src && !unavailable ? (
    item.type.startsWith("video/") ? (
      <video
        src={src}
        muted
        preload="metadata"
        draggable={false}
        onError={() => setFailedSource(src)}
      />
    ) : (
      <img src={src} alt={item.name} draggable={false} onError={() => setFailedSource(src)} />
    )
  ) : (
    <div className="editor-media-placeholder" role="img" aria-label={item.name}>
      {unavailable ? "Не удалось открыть файл" : "Загружаем…"}
    </div>
  );
}
function RemovePhotoIcon() {
  return (
    <svg
      width="20"
      height="20"
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="2"
      strokeLinecap="round"
      aria-hidden="true"
    >
      <path d="m6 6 12 12M18 6 6 18" stroke="#fff" strokeWidth="4.5" />
      <path d="m6 6 12 12M18 6 6 18" strokeWidth="2.8" />
    </svg>
  );
}

export function ProductPhotoGallery({
  items,
  cover,
  variantLabel,
  inherited,
  busy,
  disabled = false,
  canRemove = () => true,
  error,
  onChange,
  onRemove,
  onUpload,
}: {
  items: DraftMedia[];
  cover?: DraftMedia;
  variantLabel: string;
  inherited: boolean;
  busy: boolean;
  disabled?: boolean;
  canRemove?: (item: DraftMedia) => boolean;
  error: string;
  onChange: (items: DraftMedia[]) => void;
  onRemove: (id: string) => void;
  onUpload: (files: FileList | null) => void;
}) {
  const locked = busy || disabled;
  const [dragged, setDragged] = useState<string | null>(null);
  const [over, setOver] = useState<string | null>(null);
  const [notice, setNotice] = useState("");
  const touch = useRef<{ id: string; x: number; y: number; moved: boolean } | null>(null);
  const photos = items.filter((item) => item.id !== cover?.id);
  const finish = (id: string, target: string | null) => {
    if (locked) return;
    const result = moveGalleryPhoto(items, id, target);
    if (result !== items) {
      onChange(result);
      setNotice(target === null ? "Обложка обновлена" : "Порядок фото обновлён");
    }
    setDragged(null);
    setOver(null);
  };
  const start = (event: DragEvent, id: string) => {
    if (locked) {
      event.preventDefault();
      return;
    }
    event.dataTransfer.setData("application/x-ficusin-photo", id);
    event.dataTransfer.effectAllowed = "move";
    setDragged(id);
  };
  const dragOver = (event: DragEvent, target: string) => {
    if (locked || !event.dataTransfer.types.includes("application/x-ficusin-photo")) return;
    event.preventDefault();
    event.dataTransfer.dropEffect = "move";
    setOver(target);
  };
  const drop = (event: DragEvent, target: string | null) => {
    event.preventDefault();
    event.stopPropagation();
    const id = event.dataTransfer.getData("application/x-ficusin-photo");
    if (id) finish(id, target);
    else {
      setDragged(null);
      setOver(null);
    }
  };
  const pointerDown = (event: PointerEvent<HTMLButtonElement>, id: string) => {
    if (event.pointerType !== "touch" || locked) return;
    touch.current = { id, x: event.clientX, y: event.clientY, moved: false };
    event.currentTarget.setPointerCapture(event.pointerId);
  };
  const pointerMove = (event: PointerEvent<HTMLButtonElement>) => {
    const current = touch.current;
    if (!current) return;
    if (Math.hypot(event.clientX - current.x, event.clientY - current.y) > 8) {
      current.moved = true;
      setDragged(current.id);
      const target = document
        .elementFromPoint(event.clientX, event.clientY)
        ?.closest<HTMLElement>("[data-photo-drop]");
      setOver(target?.dataset.photoDrop || null);
    }
  };
  const pointerUp = (event: PointerEvent<HTMLButtonElement>) => {
    const current = touch.current;
    touch.current = null;
    if (!current) return;
    if (event.currentTarget.hasPointerCapture(event.pointerId))
      event.currentTarget.releasePointerCapture(event.pointerId);
    if (current.moved) {
      event.preventDefault();
      const target = document
        .elementFromPoint(event.clientX, event.clientY)
        ?.closest<HTMLElement>("[data-photo-drop]")?.dataset.photoDrop;
      if (target) finish(current.id, target === "cover" ? null : target);
      else {
        setDragged(null);
        setOver(null);
      }
    }
  };
  const cleanup = () => {
    touch.current = null;
    setDragged(null);
    setOver(null);
  };
  return (
    <aside
      id="product-media"
      tabIndex={-1}
      className="editor-gallery"
      aria-label="Фото и видео товара"
    >
      <div className="editor-gallery-heading">
        <h2>Обложка</h2>
      </div>
      <div
        className={`editor-gallery-cover${over === "cover" ? " is-drag-over" : ""}`}
        data-photo-drop="cover"
        draggable={Boolean(cover) && !locked}
        onDragStart={(event) => cover && start(event, cover.id)}
        onDragOver={(event) => dragOver(event, "cover")}
        onDragLeave={(event) => {
          if (!event.currentTarget.contains(event.relatedTarget as Node | null)) setOver(null);
        }}
        onDrop={(event) => drop(event, null)}
        onDragEnd={cleanup}
      >
        {cover ? (
          <>
            <Preview key={cover.id} item={cover} />
            <span className="editor-cover-info" tabIndex={0} aria-label="Главное фото">
              <span aria-hidden="true">🔥</span>
              <span>Главная</span>
              <span role="tooltip">Главное фото</span>
            </span>
            <button
              type="button"
              className="editor-photo-delete"
              aria-label="Удалить обложку"
              draggable={false}
              disabled={locked || !canRemove(cover)}
              onDragStart={(event) => {
                event.preventDefault();
                event.stopPropagation();
              }}
              onClick={() => onRemove(cover.id)}
            >
              <RemovePhotoIcon />
            </button>
          </>
        ) : (
          <div className="editor-media-placeholder">
            <svg
              width="36"
              height="36"
              viewBox="0 0 24 24"
              fill="none"
              stroke="currentColor"
              strokeWidth="1.3"
              aria-hidden="true"
            >
              <rect x="3" y="3" width="18" height="18" rx="3" />
              <circle cx="8" cy="8" r="1.5" />
              <path d="m3 17 5-5 4 4 4-5 5 6" />
            </svg>
            <span>Перетащите сюда фото для обложки</span>
          </div>
        )}
        {over === "cover" && <span className="editor-cover-drop-label">Сделать обложкой</span>}
      </div>
      <div className="editor-gallery-heading editor-variant-photo-heading">
        <h2>Фото варианта</h2>
        <span>
          {variantLabel}
          {inherited ? " · общие фото" : ""}
        </span>
      </div>
      <div className="editor-gallery-thumbs">
        {photos.map((item, i) => (
          <div
            key={item.id}
            className={`editor-photo-item${dragged === item.id ? " is-dragging" : ""}${over === item.id ? " is-drag-over" : ""}`}
            data-photo-drop={item.id}
            onDragOver={(event) => dragOver(event, item.id)}
            onDrop={(event) => drop(event, item.id)}
          >
            <button
              type="button"
              className="editor-photo-thumb"
              draggable={!locked}
              aria-label={`Фото ${i + 1}: ${item.name}. Перетащите для изменения порядка или на обложку`}
              disabled={locked}
              onDragStart={(event) => start(event, item.id)}
              onDragEnd={cleanup}
              onPointerDown={(event) => pointerDown(event, item.id)}
              onPointerMove={pointerMove}
              onPointerUp={pointerUp}
              onPointerCancel={cleanup}
              onKeyDown={(event) => {
                if (event.altKey && (event.key === "ArrowLeft" || event.key === "ArrowRight")) {
                  event.preventDefault();
                  const index = items.findIndex((photo) => photo.id === item.id);
                  const destination =
                    event.key === "ArrowLeft" ? items[index - 1] : items[index + 2];
                  if (destination) finish(item.id, destination.id);
                  else if (event.key === "ArrowRight" && index < items.length - 1) {
                    onChange([...items.filter((photo) => photo.id !== item.id), item]);
                    setNotice("Порядок фото обновлён");
                  }
                }
              }}
            >
              <Preview item={item} />
              {item.type.startsWith("video/") && <span className="editor-video-badge">Видео</span>}
            </button>
            <button
              type="button"
              className="editor-photo-delete"
              aria-label={`Удалить фото ${i + 1}: ${item.name}`}
              disabled={locked || !canRemove(item)}
              onClick={() => onRemove(item.id)}
            >
              <RemovePhotoIcon />
            </button>
          </div>
        ))}
        <label
          className="editor-upload-thumb"
          aria-label="Добавить фото или видео"
          title="Добавить фото или видео"
        >
          <span aria-hidden="true">+</span>
          <input
            type="file"
            aria-label="Добавить фото или видео"
            accept="image/*,video/*"
            multiple
            disabled={locked}
            onChange={(event) => {
              onUpload(event.target.files);
              event.target.value = "";
            }}
          />
        </label>
      </div>
      <span className="editor-photo-live" role="status">
        {busy ? "Сохраняем файлы…" : notice}
      </span>
      {error && (
        <p role="alert" className="editor-alert">
          {error}
        </p>
      )}
    </aside>
  );
}
