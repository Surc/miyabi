/* eslint-disable no-control-regex -- Reject control characters at the HTTP boundary. */
import { protocol, engineRevision, bridgeVersion, requiredCapabilities } from "./compatibility.generated.mjs";
export { protocol };
export const revision = engineRevision;
export const health = { protocol, revision, bridge_version: bridgeVersion, capabilities: requiredCapabilities };
export const positions = ["topLeft", "topRight", "bottomLeft", "bottomRight"];
export const hashPattern = /^[a-f0-9]{64}$/;

export function incompleteSources(results) {
  return results.some(
    (result) =>
      (result.status === "failed" && result.reason !== "not_found") ||
      (result.status === "skipped" &&
        ["missing_credential", "unavailable", "cooldown", "global_timeout", "ambiguous"].includes(
          result.skipReason,
        )),
  );
}

export function metadataInput(value) {
  if (
    !value ||
    typeof value.code !== "string" ||
    !value.code.trim() ||
    value.code.length > 128 ||
    /[\x00-\x1f]/.test(value.code) ||
    typeof value.translate !== "boolean"
  )
    throw Object.assign(new Error("Invalid metadata request"), { statusCode: 400 });
  return { code: value.code.trim(), translate: value.translate };
}

export function posterInput(value) {
  metadataInput({ code: value?.code, translate: false });
  if (
    !positions.includes(value.position) ||
    !Array.isArray(value.filenames) ||
    value.filenames.length > 200 ||
    !value.filenames.length ||
    value.filenames.some(
      (name) =>
        typeof name !== "string" ||
        !name.trim() ||
        name.length > 1024 ||
        /[\\/\x00-\x1f]/.test(name),
    ) ||
    typeof value.poster !== "string" ||
    !/^[A-Za-z0-9+/]+={0,2}$/.test(value.poster)
  )
    throw Object.assign(new Error("Invalid poster request"), { statusCode: 400 });
  const poster = Buffer.from(value.poster, "base64");
  if (!poster.length || poster.length > 20 * 1024 * 1024)
    throw Object.assign(new Error("Poster too large"), { statusCode: 413 });
  return { code: value.code.trim(), position: value.position, filenames: value.filenames, poster };
}
