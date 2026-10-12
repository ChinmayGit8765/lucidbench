import { writeFileSync } from "node:fs"
import path from "node:path"

import { expect, test } from "@playwright/test"

import { fakeServer, type FakeServer } from "../fakes"
import { BASE, calls, readDocker, startDaemon, writeState, type Daemon } from "../harness"

// Containers, power and Kubernetes against the fake docker engine (state in
// docker.json) and a fake Kubernetes API server named by a seeded kubeconfig.
let d: Daemon
let kube: FakeServer

const ago = (minutes: number) => new Date(Date.now() - minutes * 60_000).toISOString()
const meta = (name: string, extra: object = {}, ns?: string) => ({ name, ...(ns ? { namespace: ns } : {}), creationTimestamp: ago(30), ...extra })
const list = (kind: string, items: unknown[], apiVersion = "v1") => ({ json: { kind, apiVersion, metadata: {}, items } })

function kubeHandler(r: { method: string; path: string }) {
  if (r.method !== "GET") return null
  switch (r.path) {
    case "/api/v1/namespaces":
      return list("NamespaceList", [{ metadata: meta("default") }, { metadata: meta("e2e-apps") }])
    case "/api/v1/nodes":
      return list("NodeList", [
        {
          metadata: meta("lucidbench-control-plane", { labels: { "node-role.kubernetes.io/control-plane": "" } }),
          status: {
            capacity: { cpu: "4", memory: "8Gi" },
            conditions: [{ type: "Ready", status: "True" }],
            nodeInfo: { kubeletVersion: "v1.31.0", osImage: "Debian GNU/Linux 12", containerRuntimeVersion: "containerd://1.7.18" },
          },
        },
      ])
    case "/api/v1/pods":
      return list("PodList", [
        {
          metadata: meta("web-7d9f", { ownerReferences: [{ kind: "ReplicaSet", name: "web-7d9" }] }, "e2e-apps"),
          spec: { nodeName: "lucidbench-control-plane", containers: [{ name: "web", image: "nginx:1.27" }] },
          status: { phase: "Running", containerStatuses: [{ name: "web", ready: true, restartCount: 0 }] },
        },
        {
          metadata: meta("crashy-1", {}, "e2e-apps"),
          spec: { nodeName: "lucidbench-control-plane", containers: [{ name: "app", image: "example/crashy:1" }] },
          status: { phase: "Running", containerStatuses: [{ name: "app", ready: false, restartCount: 7, state: { waiting: { reason: "CrashLoopBackOff" } } }] },
        },
      ])
    case "/apis/batch/v1/jobs":
      return list("JobList", [
        {
          metadata: meta("hello-job", {}, "e2e-apps"),
          spec: { template: { spec: { containers: [{ name: "hello", image: "busybox:1.36" }] } } },
          status: { succeeded: 1, completionTime: ago(10), conditions: [{ type: "Complete", status: "True" }] },
        },
      ], "batch/v1")
    case "/api/v1/events":
      return list("EventList", [
        {
          metadata: meta("crashy-1.evt", {}, "e2e-apps"),
          type: "Warning", reason: "BackOff", message: "Back-off restarting failed container app", count: 12, lastTimestamp: ago(1),
          involvedObject: { kind: "Pod", name: "crashy-1" },
        },
      ])
  }
  return null
}

const compose = (name: string, state: string, service: string) => ({
  name, image: `example/${service}:1`, state, labels: `com.docker.compose.project=e2estack,com.docker.compose.service=${service}`, mem_bytes: 96 << 20,
})

test.beforeAll(async () => {
  kube = await fakeServer(kubeHandler)
  d = await startDaemon((dd) => {
    writeState(dd, "docker.json", {
      containers: [
        compose("e2e-web", "running", "web"),
        compose("e2e-worker", "exited", "worker"),
        { name: "lucidbench-control-plane", image: "kindest/node:v1.31.0", state: "running", labels: "io.x-k8s.kind.cluster=lucidbench", mem_bytes: 512 << 20 },
        { name: "someone-elses-cache", image: "redis:7", state: "running", labels: "" },
      ],
      images: [{ repository: "example/web", tag: "1", size: "120MB" }],
      volumes: [{ name: "e2e_data", labels: "com.docker.compose.project=e2estack" }],
    })
    writeFileSync(
      path.join(dd.data, "kubeconfig"),
      [
        "apiVersion: v1",
        "kind: Config",
        "clusters:",
        "- name: kind-lucidbench",
        "  cluster:",
        `    server: ${kube.url}`,
        "contexts:",
        "- name: kind-lucidbench",
        "  context: { cluster: kind-lucidbench, user: e2e }",
        "current-context: kind-lucidbench",
        "users:",
        "- name: e2e",
        "  user: {}",
        "",
      ].join("\n"),
    )
    return {
      prefs: true,
      extensions: ["containers", "kubernetes"],
      config: "power:\n  stacks:\n    - project: e2estack\n      mode: on-demand\n",
    }
  })
})
test.afterAll(async () => {
  await d?.stop()
  await kube?.close()
})

const send = (p: string, method: string) => fetch(`${BASE}${p}`, { method, headers: { "X-Lucid-Confirm": "yes" } })
const stateOf = (name: string) => readDocker(d).containers.find((c) => c.name === name)?.state

test("Containers groups what docker lists and a stack sleeps and wakes from its card", async ({ page }) => {
  await page.goto("/containers")
  await expect(page.getByRole("heading", { name: "e2estack" })).toBeVisible()
  await expect(page.getByText("1/2 running")).toBeVisible()
  await expect(page.getByRole("heading", { name: "Standalone containers" })).toBeVisible()
  await expect(page.getByTitle(/only acts on its own project/)).toBeVisible()
  await expect(page.getByText("kind cluster", { exact: true })).toBeVisible()

  // Sleep: stop the whole stack from its card, through the confirm dialog.
  await page.getByRole("button", { name: "Stop all" }).click()
  const dialog = page.getByRole("dialog")
  await expect(dialog).toContainText("Stop every container of e2estack?")
  await dialog.getByRole("button", { name: "Stop all" }).click()
  await expect.poll(() => stateOf("e2e-web")).toBe("exited")
  await expect(page.getByText("0/2 running")).toBeVisible()
  expect(calls(d)).toContain("docker stop e2e-web")

  // Wake it again.
  await page.getByRole("button", { name: "Start all" }).click()
  await page.getByRole("dialog").getByRole("button", { name: "Start all" }).click()
  await expect.poll(() => stateOf("e2e-worker")).toBe("running")
  await expect(page.getByText("2/2 running")).toBeVisible()

  // The images and volumes tabs read the same engine.
  await page.getByRole("tab", { name: /Images/ }).click()
  await expect(page.getByText("example/web")).toBeVisible()
  await page.getByRole("tab", { name: /Volumes/ }).click()
  await expect(page.getByText("e2e_data")).toBeVisible()
})

test("Sleep everything idle stops an on-demand stack that is up", async () => {
  expect(stateOf("e2e-web")).toBe("running")
  const r = await send("/api/power/sleep", "POST")
  expect(r.status).toBe(200)
  const body = (await r.json()) as { outcomes: { kind: string; name: string; stopped: boolean }[] }
  expect(body.outcomes).toContainEqual(expect.objectContaining({ kind: "stack", name: "e2estack", stopped: true }))
  expect(stateOf("e2e-web")).toBe("exited")
  expect(stateOf("e2e-worker")).toBe("exited")
  // Waking is the start action on the same stack.
  expect((await send("/api/power/stack/e2estack/start", "POST")).status).toBe(200)
  expect(stateOf("e2e-web")).toBe("running")
})

test("Kubernetes lists the cluster's nodes, pods, jobs and events", async ({ page }) => {
  await page.goto("/kubernetes")
  await expect(page.getByText("lucidbench-control-plane").first()).toBeVisible()
  await expect(page.getByText("v1.31.0").first()).toBeVisible()
  await expect(page.getByText("web-7d9f")).toBeVisible()
  await expect(page.getByText("CrashLoopBackOff")).toBeVisible()
  await page.getByRole("tab", { name: /Jobs/ }).click()
  await expect(page.getByText("hello-job")).toBeVisible()
  await expect(page.getByText("Completed").first()).toBeVisible()
  await page.getByRole("tab", { name: /Events/ }).click()
  await expect(page.getByText("Back-off restarting failed container app")).toBeVisible()
})
