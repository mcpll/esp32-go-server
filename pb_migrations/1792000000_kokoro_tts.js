// Retarget the seeded Italian agent from DashScope TTS to local Kokoro (openai provider).
// Idempotent: only rewrites agents still on the old cloud default.

migrate((app) => {
  const rows = app.findRecordsByFilter("agents", 'name = "Italiano"', "id", 50, 0)
  for (const agent of rows) {
    if (agent.get("tts_provider") !== "aliyun_qwen") continue
    const cfg = agent.get("tts_config") || {}
    if (cfg.language_type !== "Italian") continue
    agent.set("tts_provider", "openai")
    agent.set("tts_config", { voice: "im_nicola" })
    app.save(agent)
  }
}, (app) => {
  const rows = app.findRecordsByFilter("agents", 'name = "Italiano"', "id", 50, 0)
  for (const agent of rows) {
    if (agent.get("tts_provider") !== "openai") continue
    const cfg = agent.get("tts_config") || {}
    if (cfg.voice !== "im_nicola") continue
    agent.set("tts_provider", "aliyun_qwen")
    agent.set("tts_config", {
      model: "qwen3-tts-flash",
      voice: "Cherry",
      language_type: "Italian",
    })
    app.save(agent)
  }
})
