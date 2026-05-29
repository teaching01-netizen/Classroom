import type { Plugin } from "@opencode-ai/plugin"
import fs from "fs"
import path from "path"
import os from "os"

export default (async ({ client }) => {
  return {
    config: (cfg) => {
      const mainModel = cfg.model
      if (!mainModel) return

      const swarmPath = path.join(os.homedir(), ".config/opencode/opencode-swarm.json")
      try {
        const raw = fs.readFileSync(swarmPath, "utf-8")
        const swarm = JSON.parse(raw)
        let changed = false

        if (swarm.agents) {
          for (const agent of Object.values(swarm.agents) as any) {
            if (agent.model !== mainModel) {
              agent.model = mainModel
              agent.fallback_models = [mainModel]
              changed = true
            }
          }
        }

        if (changed) {
          fs.writeFileSync(swarmPath, JSON.stringify(swarm, null, 2))
        }
      } catch {}
    },
  }
}) satisfies Plugin
