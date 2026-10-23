#!/usr/bin/env python3
import importlib.util
import io
import json
import os
import unittest
from unittest.mock import patch
from pathlib import Path
spec = importlib.util.spec_from_file_location("pager", Path(__file__).with_name("notify-production-telegram.py"))
pager = importlib.util.module_from_spec(spec)
spec.loader.exec_module(pager)

class PagerTests(unittest.TestCase):
    def test_notification_uses_only_monitor_link_and_fixed_text(self):
        sent = []
        def send(request, timeout):
            sent.append(json.loads(request.data))
            return io.BytesIO(b'{"ok":true}')
        env = {"TELEGRAM_BOT_TOKEN":"fixture-only", "TELEGRAM_ALERT_CHAT_ID":"fixture-chat", "ALERT_EVENT":"failure", "RUN_URL":"https://github.com/avpavlo8/ficusin-store/actions/runs/1"}
        with patch.dict(os.environ, env, clear=True), patch.object(pager.urllib.request, "urlopen", send):
            pager.main()
        self.assertEqual(sent[0]["chat_id"], "fixture-chat")
        self.assertIn("внешняя проверка", sent[0]["text"])
        self.assertNotIn("fixture-only", sent[0]["text"])

    def test_invalid_target_is_rejected_without_network(self):
        env = {"TELEGRAM_BOT_TOKEN":"fixture-only", "TELEGRAM_ALERT_CHAT_ID":"fixture-chat", "ALERT_EVENT":"failure", "RUN_URL":"https://untrusted.invalid"}
        with patch.dict(os.environ, env, clear=True), patch.object(pager.urllib.request, "urlopen") as send:
            with self.assertRaises(RuntimeError): pager.main()
            send.assert_not_called()

if __name__ == "__main__": unittest.main()
