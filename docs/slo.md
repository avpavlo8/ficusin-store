# Service-level objectives

Owner: Platform and Commerce
Last reviewed: 2026-09-07

These are initial objectives, not claims about historical performance. Measure
the first 30 days, then approve or adjust them with the business owner.

| Service indicator | Objective | Sev-1 condition |
|---|---:|---|
| Storefront availability | 99.95% monthly | sustained customer-facing outage |
| Order API availability | 99.99% monthly | valid orders cannot be created |
| Valid order success | 99.9% | unexplained failure ratio above budget |
| Lost or duplicated paid orders | 0 | any confirmed case |
| Payment/order amount mismatch | 0 | any confirmed case |
| Runtime/database detection | ≤5 minutes | `/health` or `/ready` fails |
| Sev-1 recovery or safe degradation | ≤30 minutes | target exceeded |
| Mobile p75 LCP / INP / CLS | ≤2.5s / 200ms / 0.1 | two rolling windows over budget |

The scheduled `Production monitor` is the minimum external synthetic check. It
does not replace provider dashboards or an APM. Before horizontal scaling, add
central RED metrics, traces, error tracking and business counters for order,
payment, receipt, reservation, delivery and every durable worker queue.

Error budgets stop risky releases. A Sev-1 consumes the remaining budget until
the incident is understood, reconciled and guarded by a regression test or
monitor. Every exception has an owner and expiry date.
