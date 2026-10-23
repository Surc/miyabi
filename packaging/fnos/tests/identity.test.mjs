import { test } from "node:test";
import assert from "node:assert/strict";
import { identity, alive } from "../app/service/identity.mjs";

test("Linux process identity rejects reused PIDs, rebooted records, zombies and unreadable procfs", async () => {
  let boot = "boot-1",
    start = "1234",
    state = "S";
  const read = async (file) =>
    file.endsWith("boot_id")
      ? boot
      : `42 (worker (name)) ${[state, ...Array(18).fill("0"), start, "0"].join(" ")}`;
  const record = await identity(42, read);
  assert.deepEqual(record, { pid: 42, start: "1234", boot: "boot-1" });
  assert.equal(await alive(record, read), true);
  start = "5678";
  assert.equal(await alive(record, read), false);
  start = "1234";
  boot = "boot-2";
  assert.equal(await alive(record, read), false);
  boot = "boot-1";
  state = "Z";
  assert.equal(await alive(record, read), false);
  assert.equal(await alive({ ...record, pid: 1 }, read), false);
  assert.equal(
    await identity(42, async () => {
      throw Object.assign(new Error(), { code: "ENOENT" });
    }),
    null,
  );
  await assert.rejects(
    identity(42, async () => {
      throw Object.assign(new Error(), { code: "EACCES" });
    }),
    { code: "EACCES" },
  );
});
