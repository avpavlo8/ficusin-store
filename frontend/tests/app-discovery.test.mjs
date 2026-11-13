import test from "node:test";
import assert from "node:assert/strict";
import { claimInstallOffer, installOfferSeenKey } from "../src/lib/install-offer.ts";
import { notificationMutation } from "../src/lib/app-notifications.ts";
test("install offer claims a browser once, installed apps never claim it", () => {
  const data = new Map();
  const storage = {
    getItem: (key) => data.get(key) || null,
    setItem: (key, value) => data.set(key, value),
  };
  assert.equal(claimInstallOffer(storage, true), false);
  assert.equal(data.size, 0);
  assert.equal(claimInstallOffer(storage, false), true);
  assert.equal(data.get(installOfferSeenKey), "1");
  assert.equal(claimInstallOffer(storage, false), false);
});
test("notification switches mutate only their own channel and keep a shared device subscription", () => {
  const subscription = {
    endpoint: "https://push.example/device",
    toJSON: () => ({
      endpoint: "https://push.example/device",
      keys: { p256dh: "key", auth: "auth" },
    }),
  };
  assert.deepEqual(notificationMutation("orders", false, subscription), {
    url: "/api/v1/push/unsubscribe",
    method: "POST",
    body: { endpoint: subscription.endpoint },
  });
  assert.deepEqual(notificationMutation("chat", false, subscription), {
    url: "/api/v1/chat/notifications",
    method: "DELETE",
    body: { endpoint: subscription.endpoint },
  });
  assert.equal(notificationMutation("orders", true, subscription).url, "/api/v1/push/subscribe");
  assert.equal(notificationMutation("chat", true, subscription).url, "/api/v1/chat/notifications");
});

test("StrictMode or repeated visibility callbacks cannot claim the same persisted offer twice", async () => {
  const data = new Map();
  const storage = {
    getItem: (key) => data.get(key) || null,
    setItem: (key, value) => data.set(key, value),
  };
  assert.deepEqual(
    [
      claimInstallOffer(storage, false),
      claimInstallOffer(storage, false),
      claimInstallOffer(storage, false),
    ],
    [true, false, false],
  );
  // Closing/reloading retains the already-shown marker, not a weekly snooze.
  const reloaded = await import("../src/lib/install-offer.ts?reloaded");
  assert.equal(reloaded.claimInstallOffer(storage, false), false);
});
test("an installed app never touches storage and blocked storage falls back to one offer per page lifetime", async () => {
  const installed = await import("../src/lib/install-offer.ts?installed");
  const blocked = {
    getItem: () => {
      throw new Error("blocked");
    },
    setItem: () => {
      throw new Error("blocked");
    },
  };
  assert.equal(installed.claimInstallOffer(blocked, true), false);
  assert.equal(installed.claimInstallOffer(blocked, false), true);
  assert.equal(installed.claimInstallOffer(blocked, false), false);
  const full = await import("../src/lib/install-offer.ts?full-storage");
  const quota = {
    getItem: () => null,
    setItem: () => {
      throw new Error("quota");
    },
  };
  assert.equal(full.claimInstallOffer(quota, false), true);
  assert.equal(full.claimInstallOffer(quota, false), false);
});
