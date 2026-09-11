import test from "node:test";
import assert from "node:assert/strict";
import {
  aggregateSeries,
  costSummary,
  dimensionRows,
  modelRows,
  usedPercent,
  visibleAccounts,
} from "./data.js";

test("usedPercent converts remaining quota to used percentage", () => {
  assert.equal(usedPercent("quota", { limit: 100, remaining: 25, unit: "requests" }), 75);
  assert.equal(usedPercent("plan_percent_used", { used: 62, unit: "%" }), 62);
});

test("costSummary respects server-provided visibility", () => {
  const snapshot = {
    hide_costs: true,
    metrics: { window_cost: { used: 12, unit: "USD" } },
    model_usage: [{ cost_usd: 4 }],
  };
  assert.deepEqual(costSummary(snapshot), { value: 0, unit: "USD", hidden: true });
});

test("costSummary chooses the larger canonical signal", () => {
  const snapshot = {
    hide_costs: false,
    metrics: { window_cost: { used: 12, unit: "USD" } },
    model_usage: [{ cost_usd: 14.5 }],
  };
  assert.deepEqual(costSummary(snapshot), { value: 14.5, unit: "USD", hidden: false });
});

test("aggregateSeries sums matching daily points across accounts", () => {
  const snapshots = {
    one: { daily_series: { cost: [{ date: "2026-09-10", value: 2 }, { date: "2026-09-11", value: 3 }] } },
    two: { daily_series: { analytics_cost: [{ date: "2026-09-10", value: 5 }] } },
  };
  assert.deepEqual(aggregateSeries(snapshots, ["analytics_cost", "cost"]), [
    { date: "2026-09-10", value: 7 },
    { date: "2026-09-11", value: 3 },
  ]);
});

test("cost series and models omit accounts with hidden costs", () => {
  const snapshots = {
    visible: {
      hide_costs: false,
      daily_series: { cost: [{ date: "2026-09-11", value: 2 }] },
      model_usage: [{ canonical: "visible", cost_usd: 2 }],
    },
    hidden: {
      hide_costs: true,
      daily_series: { cost: [{ date: "2026-09-11", value: 99 }] },
      model_usage: [{ canonical: "hidden", cost_usd: 99 }],
    },
  };
  assert.deepEqual(aggregateSeries(snapshots, ["cost"]), [{ date: "2026-09-11", value: 2 }]);
  assert.equal(modelRows(snapshots)[0].name, "visible");
  assert.equal(modelRows(snapshots)[0].cost, 2);
});

test("modelRows ranks normalized model usage", () => {
  const rows = modelRows({
    one: { provider_id: "openrouter", model_usage: [{ canonical: "gpt-5", total_tokens: 1000, cost_usd: 2 }] },
    two: { provider_id: "claude_code", model_usage: [{ canonical: "gpt-5", total_tokens: 500, cost_usd: 1 }] },
  });
  assert.equal(rows[0].name, "gpt-5");
  assert.equal(rows[0].total, 1500);
  assert.equal(rows[0].providers.length, 2);
});

test("dimensionRows reads project and client metric names", () => {
  const snapshots = {
    one: { metrics: { project_openusage_requests: { used: 4 }, client_opencode_requests: { used: 7 } } },
  };
  assert.deepEqual(dimensionRows(snapshots, "project"), [{ name: "openusage", value: 4 }]);
  assert.deepEqual(dimensionRows(snapshots, "client"), [{ name: "opencode", value: 7 }]);
});

test("visibleAccounts applies configured visibility and order", () => {
  const bootstrap = {
    accounts: [
      { id: "alpha", provider_id: "a" },
      { id: "bravo", provider_id: "b" },
      { id: "charlie", provider_id: "c" },
    ],
    settings: { dashboard: { providers: [
      { account_id: "charlie", enabled: true },
      { account_id: "alpha", enabled: false },
    ] } },
  };
  assert.deepEqual(visibleAccounts(bootstrap, {}).map((account) => account.id), ["charlie", "bravo"]);
});
