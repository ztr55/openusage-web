import test from "node:test";
import assert from "node:assert/strict";

test("dashboard API sends URL tokens, removes them after bootstrap, and exposes 401", async () => {
  const originalWindow = globalThis.window;
  const originalFetch = globalThis.fetch;
  const requests = [];
  let replacedURL = "";

  globalThis.window = {
    location: {
      href: "http://127.0.0.1:8787/?access_token=url-token",
      search: "?access_token=url-token",
    },
    history: {
      replaceState: (_state, _title, url) => { replacedURL = String(url); },
    },
  };
  globalThis.fetch = async (url, options) => {
    requests.push({ url, options });
    if (url.endsWith("/bootstrap")) {
      return { ok: true, status: 200, json: async () => ({ request_token: "request-token" }) };
    }
    return { ok: false, status: 401, json: async () => ({ error: "web access token required" }) };
  };

  try {
    const api = await import(`./api.js?test=${Date.now()}`);
    await api.getBootstrap();
    assert.equal(requests[0].options.headers.get("Authorization"), "Bearer url-token");
    assert.equal(replacedURL, "http://127.0.0.1:8787/");

    await assert.rejects(api.getHealth(), (error) => error.name === "APIError" && error.status === 401);
    assert.equal(requests[1].options.headers.get("Authorization"), null);

    api.setAccessToken("form-token");
    await assert.rejects(api.getHealth(), (error) => error.name === "APIError" && error.status === 401);
    assert.equal(requests[2].options.headers.get("Authorization"), "Bearer form-token");
    assert.equal(requests[2].options.credentials, "same-origin");

    globalThis.fetch = async (url, options) => {
      requests.push({ url, options });
      return { ok: true, status: 200, json: async () => ({ flow_id: "flow" }) };
    };
    await api.startOAuthAuthorization("codex-cli", "codex");
    await api.completeOAuthAuthorization("codex-cli", "flow", "returned-code");
    assert.equal(requests[3].url, "/api/v1/accounts/codex-cli/oauth/start");
    assert.equal(requests[3].options.method, "POST");
    assert.deepEqual(JSON.parse(requests[3].options.body), { provider_id: "codex" });
    assert.equal(requests[4].url, "/api/v1/accounts/codex-cli/oauth/complete");
    assert.deepEqual(JSON.parse(requests[4].options.body), { flow_id: "flow", authorization_response: "returned-code" });
  } finally {
    globalThis.window = originalWindow;
    globalThis.fetch = originalFetch;
  }
});
