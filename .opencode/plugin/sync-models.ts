import type { Plugin } from "@opencode-ai/plugin"
import fs from "fs"
import path from "path"
import os from "os"

const configPath = path.join(os.homedir(), ".config/opencode/opencode.json")
const swarmPath = path.join(os.homedir(), ".config/opencode/opencode-swarm.json")

function syncSubagents(model: string) {
  try {
    const raw = fs.readFileSync(swarmPath, "utf-8")
    const swarm = JSON.parse(raw)
    let changed = false

    if (swarm.agents) {
      for (const agent of Object.values(swarm.agents) as any) {
        if (agent.model !== model) {
          agent.model = model
          agent.fallback_models = [model]
          changed = true
        }
      }
    }

    if (changed) {
      fs.writeFileSync(swarmPath, JSON.stringify(swarm, null, 2))
    }
  } catch {}
}

function persistModel(model: string) {
  try {
    const raw = fs.readFileSync(configPath, "utf-8")
    const config = JSON.parse(raw)
    if (config.model !== model) {
      config.model = model
      fs.writeFileSync(configPath, JSON.stringify(config, null, 2))
    }
  } catch {}
}

export default (async ({ client }) => {
  let currentModel = ""

  return {
    config: (cfg) => {
      currentModel = cfg.model || ""
      if (currentModel) {
        syncSubagents(currentModel)
      }
    },

    "chat.params": (input, output) => {
      const model = (output as any).model
      if (model && model !== currentModel) {
        currentModel = model
        persistModel(model)
        syncSubagents(model)
      }
    },
  }
}) satisfies Plugin
