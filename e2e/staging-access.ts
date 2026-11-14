import { request } from "@playwright/test";
import { mkdirSync, chmodSync } from "node:fs";
import { resolve, dirname } from "node:path";
import { isStagingOrigin } from "./staging-target";

export const stagingState = (owner = false) =>
  resolve(__dirname, ".staging-auth", owner ? "owner.json" : "gate.json");

export async function prepareStagingSession(owner = false) {
  const baseURL = process.env.FICUSIN_BASE_URL || "";
  const token = process.env.STAGING_ACCESS_TOKEN;
  if (
    !isStagingOrigin(baseURL) ||
    !token
  )
    throw new Error("Only isolated staging credentials are allowed");
  const client = await request.newContext({
    baseURL,
    extraHTTPHeaders: { Origin: baseURL },
  });
  try {
    const ready = await client.get("/api/v1/ready");
    if (ready.headers()["x-ficusin-environment"] !== "staging")
      throw new Error("Not an isolated staging environment");
    const gate = await client.post("/__staging/login", {
      form: { username: "staging", password: token, next: "/" },
    });
    if (
      !gate.ok() ||
      !(await client.storageState()).cookies.some(
        (cookie) => cookie.name === "__Host-ficusin_staging",
      )
    )
      throw new Error("Staging access session was not established");
    if (owner) {
      let response = await client.post("/api/v1/auth/request-code", {
        data: { phone: "+79990000901", flow: "login" },
      });
      for (
        let attempt = 0;
        response.status() === 429 && attempt < 10;
        attempt++
      ) {
        await new Promise((resolve) => setTimeout(resolve, 5000));
        response = await client.post("/api/v1/auth/request-code", {
          data: { phone: "+79990000901", flow: "login" },
        });
      }
      if (!response.ok())
        throw new Error(`Synthetic owner login failed: ${response.status()}`);
      const { checkId } = await response.json();
      const confirmed = await client.post("/api/v1/auth/verify-code", {
        data: { phone: "+79990000901", flow: "login", checkId },
      });
      if (
        confirmed.status() !== 200 ||
        !(await client.get("/api/v1/admin/products")).ok()
      )
        throw new Error("Synthetic owner session has no admin access");
    }
    const path = stagingState(owner);
    mkdirSync(dirname(path), { recursive: true });
    await client.storageState({ path });
    chmodSync(path, 0o600);
  } finally {
    await client.dispose();
  }
}
