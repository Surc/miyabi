import { createHmac } from "node:crypto";
import { configurationVersion, configurationView } from "./configuration.mjs";

export function createProfileAdapter(key, readProfile, openProfile) {
  let active;
  return {
    async snapshot() {
      const profile = await readProfile();
      const id = createHmac("sha256", key).update(JSON.stringify(profile)).digest("hex");
      if (active?.id !== id) {
        const ready = (async () => {
          const { runtime, defaults, sites } = await openProfile(profile);
          const information = { configuration_version: configurationVersion, configuration_id: id };
          return {
            ...runtime,
            information: () => information,
            configuration: () => configurationView(information, defaults, sites),
          };
        })();
        active = { id, ready };
        void ready.catch(() => {
          if (active?.ready === ready) active = undefined;
        });
      }
      return active.ready;
    },
  };
}
