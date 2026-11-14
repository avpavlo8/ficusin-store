import assert from "node:assert/strict";
import test from "node:test";
import { isStagingOrigin } from "./staging-target.ts";

test("both supported staging addresses pass the shared credential gate", () => {
  for (const origin of [
    "https://avpavlo8-ficusin-store-1b1b.twc1.net",
    "https://staging.ficusin.ru",
  ]) {
    assert.equal(isStagingOrigin(origin), true, origin);
  }
});

test("production, lookalikes and non-origin URLs cannot receive staging credentials", () => {
  for (const target of [
    "https://ficusin.ru",
    "https://www.ficusin.ru",
    "https://twc1.net",
    "https://.twc1.net",
    "https://staging.ficusin.ru.evil.invalid",
    "https://app.twc1.net.evil.invalid",
    "https://evil.invalid/?next=staging.ficusin.ru",
    "http://staging.ficusin.ru",
    "https://user:secret@staging.ficusin.ru",
    "https://staging.ficusin.ru:443",
    "https://staging.ficusin.ru:8080",
    "https://staging.ficusin.ru/",
    "https://staging.ficusin.ru/admin",
    "https://staging.ficusin.ru?token=secret",
    "https://staging.ficusin.ru#fragment",
    "not a URL",
    "",
  ]) {
    assert.equal(isStagingOrigin(target), false, target);
  }
});
