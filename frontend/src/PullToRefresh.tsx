import { useEffect, useState, type CSSProperties } from "react";
import { isStandalone } from "./lib/pwa";
import { installPullToRefresh, PULL_REFRESH_DISTANCE } from "./lib/pullToRefresh";
import "./styles/pull-to-refresh.css";

export function PullToRefresh() {
  const [distance, setDistance] = useState(0);
  const [refreshing, setRefreshing] = useState(false);
  useEffect(() => {
    if (
      !isStandalone() ||
      !(navigator.maxTouchPoints > 0 || matchMedia("(any-pointer: coarse)").matches)
    )
      return;
    let active = true;
    let timer: ReturnType<typeof setTimeout> | undefined;
    let settle: (() => void) | undefined;
    const dispose = installPullToRefresh({
      window,
      document,
      enabled: true,
      onProgress: (next) => {
        if (active) setDistance(next);
      },
      onRefresh: () =>
        new Promise<void>((resolve) => {
          settle = resolve;
          setRefreshing(true);
          timer = setTimeout(() => {
            if (!active) {
              resolve();
              return;
            }
            // Standard navigation preserves the current URL and honours existing
            // beforeunload guards. If the user cancels one, restore the indicator.
            try {
              window.location.reload();
            } catch {
              setRefreshing(false);
              resolve();
              return;
            }
            timer = setTimeout(() => {
              if (active) setRefreshing(false);
              resolve();
            }, 1000);
          }, 80);
        }),
    });
    return () => {
      active = false;
      dispose();
      clearTimeout(timer);
      settle?.();
    };
  }, []);
  const ready = distance >= PULL_REFRESH_DISTANCE;
  const visible = distance > 8 || refreshing;
  return (
    <div
      className="pull-to-refresh"
      role="status"
      aria-live="polite"
      hidden={!visible}
      data-refreshing={refreshing || undefined}
      data-ready={ready || undefined}
      style={{ "--pull-offset": `${Math.min(distance * 0.45, 70)}px` } as CSSProperties}
    >
      <svg
        width="22"
        height="22"
        viewBox="0 0 24 24"
        fill="none"
        stroke="currentColor"
        strokeWidth="2"
        strokeLinecap="round"
        strokeLinejoin="round"
        aria-hidden="true"
      >
        <path d="M20 7v5h-5" />
        <path d="M19.4 12a7.5 7.5 0 1 0-1.8 5" />
      </svg>
      <span>
        {refreshing ? "Обновляем…" : ready ? "Отпустите, чтобы обновить" : "Потяните вниз"}
      </span>
    </div>
  );
}
