#!/usr/bin/env node
import { performance } from "node:perf_hooks";

const baseURL = (process.argv[2] || process.env.BASE_URL || "http://127.0.0.1:3000").replace(/\/$/, "");
const requestCount = Number(process.env.LOAD_REQUESTS || 240);
const concurrency = Number(process.env.LOAD_CONCURRENCY || 12);
const p95BudgetMs = Number(process.env.LOAD_P95_BUDGET_MS || 1000);
const routes = [
  "/api/v1/health",
  "/api/v1/ready",
  "/api/v1/operations/health",
  "/api/v1/payments/methods",
  "/api/v1/cart",
];
const timings = [];
const failures = [];
let cursor = 0;

async function worker() {
  while (true) {
    const index = cursor++;
    if (index >= requestCount) return;
    const route = routes[index % routes.length];
    const started = performance.now();
    try {
      const response = await fetch(baseURL + route, {
        headers: { "User-Agent": "ficusin-ci-load-smoke" },
        signal: AbortSignal.timeout(5000),
      });
      const body = await response.json();
      if (!response.ok) throw new Error(`HTTP ${response.status}: ${JSON.stringify(body).slice(0, 200)}`);
      if (route === "/api/v1/cart" && (!body.items || !Array.isArray(body.lines))) {
        throw new Error("invalid cart response");
      }
      timings.push(performance.now() - started);
    } catch (error) {
      failures.push(`${route}: ${error.message}`);
    }
  }
}

await Promise.all(Array.from({ length: concurrency }, () => worker()));
timings.sort((a, b) => a - b);
const percentile = (value) => timings[Math.min(timings.length - 1, Math.ceil(timings.length * value) - 1)] || Infinity;
const p50 = percentile(0.5);
const p95 = percentile(0.95);
const p99 = percentile(0.99);
console.log(JSON.stringify({ baseURL, requests: requestCount, concurrency, failures: failures.length, p50Ms: p50, p95Ms: p95, p99Ms: p99 }));
if (failures.length) {
  console.error(failures.slice(0, 10).join("\n"));
  process.exit(1);
}
if (p95 > p95BudgetMs) {
  console.error(`p95 ${p95.toFixed(1)}ms exceeds ${p95BudgetMs}ms budget`);
  process.exit(1);
}
