import { createServer } from "node:http";
import { timingSafeEqual } from "node:crypto";
import { health, hashPattern, metadataInput, posterInput } from "./contract.mjs";

// The HTTP boundary depends on an adapter, never on a particular MDCz module.
// One active job bounds native image memory and source-site traffic on J4125.
export function createBridge(adapter, token) {
  if (!token || token.length < 24 || /[\r\n]/.test(token))
    throw new Error("Use a bridge key of at least 24 characters");
  let busy = false;
  return createServer(async (req, res) => {
    const send = (status, value, type = "application/json") => {
      if (res.destroyed) return;
      res.writeHead(status, {
        "content-type": type,
        "cache-control": "no-store",
        "x-content-type-options": "nosniff",
      });
      res.end(type === "application/json" ? JSON.stringify(value) : value);
    };
    const provided = Buffer.from(req.headers.authorization ?? "");
    const expected = Buffer.from(`Bearer ${token}`);
    if (provided.length !== expected.length || !timingSafeEqual(provided, expected))
      return send(401, { error: "Unauthorized" });
    if (req.method === "GET" && req.url === "/v1/health")
      return send(200, { ...health, ...adapter.information?.() });
    if (req.method === "GET" && req.url === "/v1/configuration" && adapter.configuration)
      return send(200, { ...health, ...adapter.configuration() });
    const imageID =
      req.method === "GET" && req.url?.startsWith("/v1/images/") ? req.url.slice(11) : "";
    const metadata = req.method === "POST" && req.url === "/v1/metadata";
    const poster = req.method === "POST" && req.url === "/v1/poster";
    const configure = req.method === "PUT" && req.url === "/v1/configuration" && adapter.configure;
    if (!metadata && !poster && !configure && !hashPattern.test(imageID))
      return send(404, { error: "Not found" });
    if (busy) {
      res.setHeader("retry-after", "5");
      return send(503, { error: "Busy; retry later" });
    }
    busy = true;
    const abort = new AbortController();
    const timeout = setTimeout(() => abort.abort(), 150_000);
    abort.signal.addEventListener(
      "abort",
      () => {
        if (!req.complete) req.destroy();
      },
      { once: true },
    );
    const disconnected = () => {
      if (!res.writableEnded) abort.abort();
    };
    res.on("close", disconnected);
    try {
      if (imageID) {
        const image = await adapter.image(imageID, abort.signal);
        return send(200, image, "image/jpeg");
      }
      const chunks = [];
      let size = 0;
      for await (const chunk of req) {
        size += chunk.length;
        if (size > (configure ? 128 * 1024 : 28 * 1024 * 1024))
          throw Object.assign(new Error("Too large"), { statusCode: 413 });
        if (abort.signal.aborted) throw new Error("Canceled");
        chunks.push(chunk);
      }
      let input;
      try {
        input = JSON.parse(Buffer.concat(chunks).toString());
      } catch {
        throw Object.assign(new Error("Invalid JSON"), { statusCode: 400 });
      }
      const result = configure
        ? await adapter.configure(input)
        : metadata
          ? await adapter.metadata(metadataInput(input), abort.signal)
          : await adapter.poster(posterInput(input), abort.signal);
      if (abort.signal.aborted) return send(504, { error: "Canceled or timed out" });
      send(200, { ...health, ...result });
    } catch (error) {
      // Site exceptions may contain proxy URLs or cookies; expose only category.
      const status = [400, 404, 409, 413].includes(error?.statusCode)
        ? error.statusCode
        : abort.signal.aborted
          ? 504
          : 502;
      send(status, { error: status === 502 ? "MDCz operation failed" : "Request rejected" });
    } finally {
      clearTimeout(timeout);
      res.off("close", disconnected);
      busy = false;
    }
  });
}
