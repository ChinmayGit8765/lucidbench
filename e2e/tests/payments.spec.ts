import { expect, test } from "@playwright/test"

import { fakeKey, fakeServer, type FakeServer } from "../fakes"
import { BASE, makeRepo, projectYaml, startDaemon, type Daemon } from "../harness"

// A fake Stripe on a loopback port; the daemon reaches it through
// integrations.stripe.api_url. Test mode only: a live key must be refused.
let d: Daemon | undefined
let stripe: FakeServer
let n = 0

const list = (data: unknown[]) => ({ json: { object: "list", data, has_more: false } })

function stripeHandler(r: { method: string; path: string }) {
  if (r.method === "POST") {
    const kind = r.path.split("/").pop()!
    const id = `${kind}_fake${++n}`
    if (r.path === "/v1/webhook_endpoints") return { json: { id, secret: "whsec_" + "fakesecret" + n } }
    if (r.path === "/v1/payment_links") return { json: { id, url: `https://buy.example.test/${id}` } }
    return { json: { id } }
  }
  switch (r.path) {
    case "/v1/account":
      return { json: { id: "acct_fake", country: "AU", charges_enabled: true, payouts_enabled: true, business_profile: { name: "Fixture Studio" } } }
    case "/v1/balance":
      return { json: { available: [{ amount: 128450, currency: "aud" }], pending: [] } }
    case "/v1/payment_intents":
      return list([{ id: "pi_fake1", amount: 4900, amount_received: 4900, currency: "aud", status: "succeeded", created: Math.floor(Date.now() / 1000) - 3600, description: "Pro plan e2e" }])
    case "/v1/products":
    case "/v1/prices":
    case "/v1/payment_links":
    case "/v1/webhook_endpoints":
      return list([])
  }
  return null
}

async function boot(key: string) {
  stripe = await fakeServer(stripeHandler)
  d = await startDaemon((dd) => ({
    prefs: true,
    extensions: ["payments"],
    projects: `version: 1\nprojects:\n${projectYaml("shop", "Shop", makeRepo(dd, "shop"))}`,
    env: { E2E_STRIPE_KEY: key, LUCID_INTEGRATIONS_STRIPE_KEY: "env:E2E_STRIPE_KEY", LUCID_INTEGRATIONS_STRIPE_API_URL: stripe.url },
  }))
}

async function shutdown() {
  await d?.stop()
  d = undefined
  await stripe?.close()
}

test.afterEach(shutdown)

test("a test key runs the setup wizard against a fake Stripe and the audit log records it", async ({ page }) => {
  await boot(fakeKey("sk", "test"))
  await page.goto("/payments")
  await expect(page.getByText("TEST", { exact: true }).first()).toBeVisible()
  await expect(page.getByText("Fixture Studio")).toBeVisible()
  await expect(page.getByText("Pro plan e2e")).toBeVisible()

  await page.getByRole("tab", { name: "Set up payments" }).click()
  await page.getByLabel("Product name").fill("Gadget e2e")
  await page.getByLabel("Price 1 amount").fill("19.00")
  await page.getByLabel("Price 1 currency").fill("usd")
  await page.getByLabel("Success URL").fill("https://example.com/thanks")
  await page.getByLabel("Webhook URL").fill("https://example.com/hooks/stripe")
  await page.getByRole("button", { name: "Review the plan" }).click()

  await expect(page.getByRole("heading", { name: "Review the plan for shop" })).toBeVisible()
  await expect(page.getByText("TEST key")).toBeVisible()
  // Building the plan wrote nothing.
  expect(stripe.seen.filter((s) => s.method === "POST")).toHaveLength(0)

  await page.getByRole("button", { name: "Create in test mode" }).click()
  const dialog = page.getByRole("dialog")
  await expect(dialog).toContainText("Create these objects in your Stripe test account?")
  await dialog.getByRole("button", { name: "Create in test mode" }).click()

  await expect(page.getByText("All created")).toBeVisible()
  await expect(page.getByTestId("webhook-secret")).toContainText("whsec_")
  const posts = stripe.seen.filter((s) => s.method === "POST")
  expect(posts.map((p) => p.path)).toEqual(["/v1/products", "/v1/prices", "/v1/payment_links", "/v1/webhook_endpoints"])
  expect(posts[0].headers.authorization).toMatch(/^Bearer sk_test_/)
  expect(posts[0].headers["idempotency-key"]).toBeTruthy()
  expect(new URLSearchParams(posts[0].body).get("name")).toBe("Gadget e2e")

  await page.getByRole("tab", { name: "Audit log" }).click()
  await expect(page.getByRole("cell", { name: "product.create" })).toBeVisible()
  await expect(page.getByRole("cell", { name: "webhook_endpoint.create" })).toBeVisible()
  // The signing secret never reaches the audit log.
  const audit = await (await fetch(`${BASE}/api/payments/audit`)).text()
  expect(audit).not.toContain("whsec_")
})

test("a live key is read-only: the wizard refuses and the execute route answers 403", async ({ page }) => {
  await boot(fakeKey("sk", "live"))
  await page.goto("/payments")
  await expect(page.getByText("LIVE", { exact: true }).first()).toBeVisible()
  await page.getByRole("tab", { name: "Set up payments" }).click()
  await expect(page.getByText("Live mode writes are not supported in this version.")).toBeVisible()

  await page.getByLabel("Product name").fill("Gadget live e2e")
  await page.getByLabel("Price 1 amount").fill("5")
  await page.getByLabel("Success URL").fill("https://example.com/thanks")
  await page.getByRole("button", { name: "Review the plan" }).click()
  await expect(page.getByText("This plan cannot be run.")).toBeVisible()
  await expect(page.getByRole("button", { name: "Create in test mode" })).toBeDisabled()

  // Even a hand-made execute is refused before Stripe is called.
  const r = await fetch(`${BASE}/api/payments/plan/execute`, {
    method: "POST",
    headers: { "Content-Type": "application/json", "X-Lucid-Confirm": "yes" },
    body: JSON.stringify({ id: "0123456789abcdef", digest: "x", project: "shop", mode: "live", writable: true, request: {}, steps: [], warnings: [] }),
  })
  expect(r.status).toBe(403)
  expect(await r.text()).toContain("live mode writes are not supported")
  expect(stripe.seen.filter((s) => s.method === "POST")).toHaveLength(0)
})
