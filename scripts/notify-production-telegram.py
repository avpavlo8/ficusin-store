#!/usr/bin/env python3
"""External outage pager. Never print credentials, response bodies or bot URLs."""
import json
import os
import sys
import urllib.request


def main():
    token = os.environ.get("TELEGRAM_BOT_TOKEN", "").strip()
    chat = os.environ.get("TELEGRAM_ALERT_CHAT_ID", "").strip()
    if not token or not chat:
        raise RuntimeError("Telegram alert configuration missing")
    event = os.environ.get("ALERT_EVENT", "")
    if event not in {"failure", "recovery"}:
        raise RuntimeError("Unknown alert event")
    run_url = os.environ.get("RUN_URL", "")
    if not run_url.startswith("https://github.com/avpavlo8/ficusin-store/actions/runs/"):
        raise RuntimeError("Invalid monitor run URL")
    text = ("Фикусин: внешняя проверка сайта не прошла. Проверьте доступность магазина. "
            if event == "failure" else "Фикусин: внешняя проверка сайта восстановилась. ") + run_url
    payload = json.dumps({"chat_id": chat, "text": text, "disable_web_page_preview": True}).encode()
    request = urllib.request.Request(
        "https://api.telegram.org/bot" + token + "/sendMessage",
        data=payload, headers={"Content-Type": "application/json"}, method="POST")
    with urllib.request.urlopen(request, timeout=10) as response:
        data = json.load(response)
    if data.get("ok") is not True:
        raise RuntimeError("Telegram rejected the notification")
    print("Production monitor notification delivered")


if __name__ == "__main__":
    try:
        main()
    except Exception:
        # urllib exceptions include the full URL containing the token.
        print("Production monitor Telegram notification failed; see the incident in GitHub", file=sys.stderr)
        sys.exit(1)
