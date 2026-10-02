import { expect, test, type Page } from "@playwright/test";
const SITE = "site-plan", FLOOR = "floor-plan", SECOND = "floor-second";
const json = (body: unknown) => ({ contentType: "application/json", body: JSON.stringify(body) });
/** A valid two-page PDF with different page colors proves selected-page rasterization. */
function twoPagePDF() {
  const objects = ["<< /Type /Catalog /Pages 2 0 R >>", "<< /Type /Pages /Kids [3 0 R 4 0 R] /Count 2 >>",
    "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 100 80] /Resources << >> /Contents 5 0 R >>",
    "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 100 80] /Resources << >> /Contents 6 0 R >>",
    ...["1 0 0 rg 0 0 100 80 re f\n", "0 1 0 rg 0 0 100 80 re f\n"].map(stream => `<< /Length ${Buffer.byteLength(stream)} >>\nstream\n${stream}endstream`)];
  let out = "%PDF-1.4\n";
  const offsets = [0];
  objects.forEach((object, n) => { offsets.push(Buffer.byteLength(out)); out += `${n + 1} 0 obj\n${object}\nendobj\n`; });
  const xref = Buffer.byteLength(out);
  out += `xref\n0 7\n0000000000 65535 f \n${offsets.slice(1).map(offset => String(offset).padStart(10, "0") + " 00000 n \n").join("")}trailer\n<< /Size 7 /Root 1 0 R >>\nstartxref\n${xref}\n%%EOF\n`;
  return Buffer.from(out);
}
async function fixture(page: Page) {
  const external: string[] = [];
  const requests: string[] = [];
  const uploads: Buffer[] = [];
  const placements: Record<string, unknown>[] = [];
  const entities: Record<string, Record<string, unknown>[]> = { [FLOOR]: [], [SECOND]: [] };
  let image: Buffer | undefined, revision = 1, conflict = false;
  await page.route("**/*", route => {
    const url = new URL(route.request().url());
    if (url.hostname !== "127.0.0.1") {
      external.push(url.origin);
      return route.abort();
    }
    requests.push(url.pathname);
    return route.continue();
  });
  await page.routeWebSocket("**/ws", socket => socket.close());
  await page.route("**/api/v1/**", async (route) => {
    const request = route.request(), url = new URL(request.url()), path = url.pathname;
    if(url.hostname!=="127.0.0.1"){external.push(url.origin);return route.abort();}
    requests.push(path);
    if (path === "/api/v1/me")
      return route.fulfill(json({ id: "fixture-user", username: "fixture", tenant_id: "fixture-tenant", grants: ["maps.view", "maps.edit", "maps.edit_device"].map(permission => ({ permission, effect: "allow", scope_type: "platform" })) }));
    if (path === "/api/v1/features")
      return route.fulfill(json({ maps: true }));
    if (path === "/api/v1/maps/overview")
      return route.fulfill(json({ items: [{ id: SITE, name: "Fixture city", camera_count: 2, online_cameras: 1, offline_cameras: 1, degraded_cameras: 0, alarm_count: 0 }] }));
    if (path === `/api/v1/maps/sites/${SITE}`)
      return route.fulfill(json({ id: SITE, name: "Fixture city", buildings: [{ id: "building", site_id: SITE, name: "Fixture plant", revision: 1, floors: [{ id: FLOOR, building_id: "building", name: "Main plan", ordinal: 0, revision, plan_key: image ? "opaque-never-a-url" : null, plan_width_px: image ? image.readUInt32BE(16) : 0, plan_height_px: image ? image.readUInt32BE(20) : 0 }, { id: SECOND, building_id: "building", name: "Second plan", ordinal: 1, revision: 1 }] }] }));
    if (path === `/api/v1/maps/sites/${SITE}/floors/${FLOOR}/plan`) {
      if (request.method() === "PUT") {
        expect(request.headers()["authorization"]).toBe("Bearer browser-fixture-not-a-real-token");
        expect(request.headers()["content-type"]).toBe("image/png");
        expect(request.headers()["if-match"]).toBe(String(revision));
        if (conflict) {
          revision++;
          conflict = false;
          return route.fulfill({ status: 409, ...json({ message: "Map revision changed" }) });
        }
        image = request.postDataBuffer()!;
        uploads.push(image);
        revision++;
        return route.fulfill(json({ id: FLOOR, building_id: "building", name: "Main plan", ordinal: 0, revision }));
      }
      return image ? route.fulfill({ contentType: "image/png", body: image }) : route.fulfill({ status: 404, ...json({ message: "No plan" }) });
    }
    if (path === `/api/v1/maps/sites/${SITE}/entities`) {
      expect(url.searchParams.has("floor_id")).toBe(true);
      return route.fulfill(json({ revision: 1, entities: entities[url.searchParams.get("floor_id")!] ?? [] }));
    }
    if (path === "/api/v1/maps/unplaced") {
      const floor = url.searchParams.get("floor_id")!;
      expect(floor).toBeTruthy();
      return route.fulfill(json({ cameras: [{ id: "camera-offline", name: "Offline camera", site_id: SITE, status: "offline" }, { id: "camera-online", name: "Online camera", site_id: SITE, status: "online" }].filter(camera => !entities[floor]?.some(entity => entity.id === camera.id)) }));
    }
    if (path.startsWith("/api/v1/maps/placements/camera/")) {
      const body = request.postDataJSON() as Record<string, unknown>;
      placements.push(body);
      expect(body).not.toHaveProperty("lat");
      expect(body).not.toHaveProperty("lng");
      const floor = body.floor_id as string, id = path.split("/").at(-1)!;
      const previous = entities[floor]!.find(entity => entity.id === id);
      if (previous)
        expect(request.headers()["if-match"]).toBe(`"${previous.rev}"`);
      else
        expect(request.headers()["if-match"]).toBeUndefined();
      const entity = { id, t: "camera", site: SITE, name: id === "camera-offline" ? "Offline camera" : "Online camera", st: id === "camera-offline" ? "offline" : "online", pos: { k: "floor", floor_id: floor, x: body.x, y: body.y }, rev: Number(previous?.rev ?? 0) + 1, alarms: 0, cam: { bearing: 0, fov: 60, range: 100, type: "fixed", ptz: false, lpr: false } };
      entities[floor] = [...entities[floor]!.filter(item => item.id !== id), entity];
      return route.fulfill(json({ revision: entity.rev }));
    }
    return route.fulfill(json({ items: [] }));
  });
  await page.addInitScript(() => sessionStorage.setItem("openvms.token", "browser-fixture-not-a-real-token"));
  await page.goto(`/maps?site=${SITE}&floor=${FLOOR}&mode=edit`);
  await expect(page.getByTestId("floor-viewport")).toBeVisible();
  return { external, requests, uploads, placements, entities, setConflict: () => { conflict = true; } };
}
async function pngPixel(page: Page, bytes: Buffer) {
  expect(bytes.subarray(0, 8)).toEqual(Buffer.from([137, 80, 78, 71, 13, 10, 26, 10]));
  return page.evaluate(async (values) => { const bitmap = await createImageBitmap(new Blob([new Uint8Array(values)], { type: "image/png" })); const canvas = document.createElement("canvas"); canvas.width = bitmap.width; canvas.height = bitmap.height; const context = canvas.getContext("2d")!; context.drawImage(bitmap, 0, 0); return { width: bitmap.width, height: bitmap.height, pixel: Array.from(context.getImageData(5, 5, 1, 1).data) }; }, Array.from(bytes));
}
test("private custom plan UI converts PNG, restricted SVG and PDF page two with only local resources", async ({ page }) => {
  const errors: string[] = [];
  page.on("pageerror", error => errors.push(error.message));
  const f = await fixture(page);
  const png = Buffer.from(await page.evaluate(async () => { const canvas = document.createElement("canvas"); canvas.width = 40; canvas.height = 20; const context = canvas.getContext("2d")!; context.fillStyle = "#ff0000"; context.fillRect(0, 0, 40, 20); const blob = await new Promise<Blob>(resolve => canvas.toBlob(blob => resolve(blob!), "image/png")); return Array.from(new Uint8Array(await blob.arrayBuffer())); }));
  for (const input of [{ name: "actual.png", mimeType: "image/png", buffer: png, color: [255, 0, 0] },
    { name: "restricted.svg", mimeType: "image/svg+xml", buffer: Buffer.from('<svg xmlns="http://www.w3.org/2000/svg" width="40" height="20"><script>alert(1)</script><image href="https://forbidden.invalid/image.png"/><rect width="40" height="20" fill="#0000ff"/></svg>'), color: [0, 0, 255] },
    { name: "two-page.pdf", mimeType: "application/pdf", buffer: twoPagePDF(), color: [0, 255, 0] }]) {
    await page.getByLabel("Archivo de plano").setInputFiles(input);
    await expect(page.getByAltText("Vista previa del plano")).toBeVisible();
    if (input.name.endsWith(".svg"))
      await expect(page.getByText("Unsupported or active SVG content was removed. Check the preview before saving.")).toBeVisible();
    if (input.name.endsWith(".pdf")) {
      await page.getByLabel("Página PDF").selectOption("2");
      await expect(page.getByRole("button", { name: "Guardar fondo" })).toBeEnabled();
    }
    const count = f.uploads.length;
    await page.getByRole("button", { name: "Guardar fondo" }).click();
    await expect.poll(() => f.uploads.length).toBe(count + 1);
    const decoded = await pngPixel(page, f.uploads.at(-1)!);
    expect(decoded.pixel.slice(0, 3)).toEqual(input.color);
    if (input.name.endsWith(".pdf")) {
      expect(decoded.width).toBe(200);
      expect(decoded.height).toBe(160);
    }
    await expect(page.getByRole("img", { name: "Map background" })).toBeVisible();
  }
  expect(f.requests.some(path => /pdf\.worker.*\.mjs$/.test(path))).toBe(true);
  expect(f.requests).not.toContain("/api/v1/maps/config");
  expect(f.external).toEqual([]);
  expect(errors).toEqual([]);
  f.setConflict();
  await page.getByLabel("Archivo de plano").setInputFiles({ name: "conflict.png", mimeType: "image/png", buffer: png });
  await expect(page.getByAltText("Vista previa del plano")).toBeVisible();
  await page.getByRole("button", { name: "Guardar fondo" }).click();
  await expect(page.getByText(/The background changed on the server/)).toBeVisible();
  expect(f.uploads).toHaveLength(3);
});
test("floor drag, fixed-size gray markers, explicit save/reload and independent maps", async ({ page }) => {
  const f = await fixture(page);
  const canvas = page.getByTestId("floor-viewport");
  await page.getByRole("button", { name: "Offline camera", exact: true }).dragTo(canvas, { targetPosition: { x: 350, y: 180 } });
  await expect(page.getByText("1 unsaved changes")).toBeVisible();
  expect(f.placements).toHaveLength(0);
  await page.getByRole("button", { name: "Save placements (1)" }).click();
  await expect.poll(() => f.placements.length).toBe(1);
  await expect(page.getByText("1 unsaved changes")).not.toBeVisible();
  await expect(page.locator('[data-camera-id="camera-offline"]')).toBeVisible();
  const first = f.placements[0]!;
  expect(Number(first.x)).toBeGreaterThan(0);
  expect(Number(first.y)).toBeGreaterThan(0);
  await page.reload();
  const marker = page.getByRole("button", { name: "Offline camera", exact: true });
  await expect(marker).toHaveAttribute("data-connection", "offline");
  const bounds = await marker.boundingBox();
  await page.getByRole("button", { name: "Zoom in" }).click();
  const zoomed = await marker.boundingBox();
  expect(zoomed!.width).toBe(bounds!.width);
  await page.getByRole("button", { name: "Fit map" }).click();
  const box = await marker.boundingBox();
  await page.mouse.move(box!.x + box!.width / 2, box!.y + box!.height / 2);
  await page.mouse.down();
  await page.mouse.move(box!.x + box!.width / 2 + 50, box!.y + box!.height / 2 + 30, { steps: 6 });
  await page.mouse.up();
  await expect(page.getByText("1 unsaved changes")).toBeVisible();
  await page.getByLabel("Map", { exact: true }).selectOption(SECOND);
  await expect(page.getByText(/This map has unsaved changes/)).toBeVisible();
  await expect(page.getByLabel("Map", { exact: true })).toHaveValue(FLOOR);
  await page.getByRole("button", { name: "Keep editing" }).click();
  await page.getByRole("button", { name: "Save placements (1)" }).click();
  await expect.poll(() => f.placements.length).toBe(2);
  await expect(page.getByText("1 unsaved changes")).not.toBeVisible();
  await page.getByLabel("Map", { exact: true }).selectOption(SECOND);
  await expect(page.getByRole("region", { name: "Map: Second plan" })).toBeVisible();
  await page.getByRole("button", { name: "Offline camera", exact: true }).dragTo(page.getByTestId("floor-viewport"), { targetPosition: { x: 350, y: 180 } });
  await page.getByRole("button", { name: "Save placements (1)" }).click();
  await expect.poll(() => f.placements.length).toBe(3);
  await expect(page.getByText("1 unsaved changes")).not.toBeVisible();
  expect(f.entities[FLOOR]).toHaveLength(1);
  expect(f.entities[SECOND]).toHaveLength(1);
  expect(f.placements.map(item => item.floor_id)).toEqual([FLOOR, FLOOR, SECOND]);
  expect(f.external).toEqual([]);
});
test("touch cards drop directly onto the map and cancelled marker movement restores its position", async ({ browser }) => {
  const context = await browser.newContext({ hasTouch: true, viewport: { width: 1280, height: 900 } });
  const page = await context.newPage();
  try {
    const f = await fixture(page);
    const card = await page.getByRole("button", { name: "Offline camera", exact: true }).boundingBox();
    const canvas = await page.getByTestId("floor-viewport").boundingBox();
    const cdp = await context.newCDPSession(page);
    const start = { x: card!.x + card!.width / 2, y: card!.y + card!.height / 2 };
    const end = { x: canvas!.x + canvas!.width / 2, y: canvas!.y + canvas!.height / 2 };
    await cdp.send("Input.dispatchTouchEvent", { type: "touchStart", touchPoints: [{ ...start, id: 1 }] });
    await cdp.send("Input.dispatchTouchEvent", { type: "touchMove", touchPoints: [{ ...end, id: 1 }] });
    await cdp.send("Input.dispatchTouchEvent", { type: "touchEnd", touchPoints: [] });
    await expect(page.getByText("1 unsaved changes")).toBeVisible();
    await page.getByRole("button", { name: "Save placements (1)" }).click();
    await expect.poll(() => f.placements.length).toBe(1);
    await expect(page.getByText("1 unsaved changes")).not.toBeVisible();
    await expect(page.locator('[data-camera-id="camera-offline"]')).toBeVisible();
    const marker = await page.getByRole("button", { name: "Offline camera", exact: true }).boundingBox();
    const m = { x: marker!.x + 20, y: marker!.y + 20 };
    await cdp.send("Input.dispatchTouchEvent", { type: "touchStart", touchPoints: [{ ...m, id: 2 }] });
    await cdp.send("Input.dispatchTouchEvent", { type: "touchMove", touchPoints: [{ x: m.x + 40, y: m.y + 30, id: 2 }] });
    await cdp.send("Input.dispatchTouchEvent", { type: "touchCancel", touchPoints: [] });
    await expect(page.getByText("1 unsaved changes")).not.toBeVisible();
    expect(f.placements).toHaveLength(1);
    expect(f.external).toEqual([]);
  }
  finally {
    await context.close();
  }
});
