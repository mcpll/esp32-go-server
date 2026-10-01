// First boot reads ADMIN_EMAIL and ADMIN_PASSWORD and creates the only console user.

migrate((app) => {
  const email = $os.getenv("ADMIN_EMAIL")
  const password = $os.getenv("ADMIN_PASSWORD")
  if (!email || !password) {
    throw new Error("ADMIN_EMAIL and ADMIN_PASSWORD are required")
  }

  const users = app.findCollectionByNameOrId("users")
  const user = new Record(users)
  user.set("email", email)
  user.set("password", password)
  user.set("passwordConfirm", password)
  user.set("verified", true)
  app.save(user)

  const onlyConsole = '@request.auth.id = "' + user.id + '"'
  users.listRule = onlyConsole
  users.viewRule = onlyConsole
  users.createRule = null
  users.updateRule = onlyConsole
  users.deleteRule = null
  app.save(users)

  const agents = new Collection({
    type: "base",
    name: "agents",
    listRule: onlyConsole,
    viewRule: onlyConsole,
    createRule: onlyConsole,
    updateRule: onlyConsole,
    deleteRule: onlyConsole,
    fields: [
      { name: "name", type: "text", required: true, max: 200 },
      { name: "prompt", type: "text", required: true, max: 20000 },
      { name: "asr_provider", type: "text", required: true, max: 100 },
      { name: "asr_config", type: "json", required: true },
      { name: "llm_provider", type: "text", required: true, max: 100 },
      { name: "llm_config", type: "json", required: true },
      { name: "tts_provider", type: "text", required: true, max: 100 },
      { name: "tts_config", type: "json", required: true },
      {
        name: "memory_mode",
        type: "select",
        required: true,
        maxSelect: 1,
        values: ["none", "short", "long"],
      },
      { name: "voiceprint_enabled", type: "bool" },
      {
        name: "speaker_chat_mode",
        type: "select",
        required: true,
        maxSelect: 1,
        values: ["off", "identified_only"],
      },
      { name: "knowledge_enabled", type: "bool" },
      { name: "mcp_service_names", type: "json" },
      { name: "openclaw", type: "json", required: true },
    ],
  })
  app.save(agents)

  const devices = new Collection({
    type: "base",
    name: "devices",
    listRule: onlyConsole,
    viewRule: onlyConsole,
    createRule: onlyConsole,
    updateRule: onlyConsole,
    deleteRule: onlyConsole,
    fields: [
      { name: "device_id", type: "text", required: true, max: 64 },
      { name: "client_id", type: "text", max: 128 },
      { name: "code", type: "text", required: true, max: 6, pattern: "^[0-9]{6}$" },
      { name: "challenge", type: "text", max: 80 },
      { name: "activated", type: "bool" },
      {
        name: "agent",
        type: "relation",
        collectionId: agents.id,
        maxSelect: 1,
        cascadeDelete: false,
      },
      { name: "note", type: "text", max: 500 },
      { name: "online", type: "bool" },
      { name: "last_active_at", type: "date" },
    ],
    indexes: [
      "CREATE UNIQUE INDEX idx_devices_device_id ON devices (device_id)",
      "CREATE UNIQUE INDEX idx_devices_code ON devices (code)",
    ],
  })
  app.save(devices)

  const settings = new Collection({
    type: "base",
    name: "settings",
    listRule: onlyConsole,
    viewRule: onlyConsole,
    createRule: onlyConsole,
    updateRule: onlyConsole,
    deleteRule: onlyConsole,
    fields: [
      { name: "key", type: "text", required: true, max: 64 },
      { name: "value", type: "json", required: true },
    ],
    indexes: [
      "CREATE UNIQUE INDEX idx_settings_key ON settings (key)",
    ],
  })
  app.save(settings)

  const agent = new Record(agents)
  agent.set("name", "Italiano")
  agent.set("prompt", "Rispondi in italiano, in una o due frasi brevi. Niente emoji, codice o elenchi.")
  agent.set("asr_provider", "aliyun_qwen3")
  agent.set("asr_config", { language: "it", auto_end: false })
  agent.set("llm_provider", "aliyun")
  agent.set("llm_config", {
    type: "openai",
    model_name: "qwen3.8-flash",
    base_url: "https://dashscope.aliyuncs.com/compatible-mode/v1",
    max_tokens: 300,
    thinking: { mode: "disabled" },
  })
  agent.set("tts_provider", "aliyun_qwen")
  agent.set("tts_config", {
    model: "qwen3-tts-flash",
    voice: "Cherry",
    language_type: "Italian",
  })
  agent.set("memory_mode", "none")
  agent.set("voiceprint_enabled", false)
  agent.set("speaker_chat_mode", "off")
  agent.set("knowledge_enabled", false)
  agent.set("mcp_service_names", [])
  agent.set("openclaw", {
    allowed: false,
    enter_keywords: ["apri openclaw", "entra in openclaw"],
    exit_keywords: ["chiudi openclaw", "esci da openclaw"],
  })
  app.save(agent)

  const settingsSeed = {
    mqtt: {
      enable: false,
      broker: "127.0.0.1",
      type: "tcp",
      port: 2883,
      client_id: "xiaozhi_server",
      username: "admin",
    },
    mqtt_server: {
      enable: false,
      listen_host: "0.0.0.0",
      listen_port: 2883,
      client_id: "xiaozhi_server",
      username: "admin",
      enable_auth: false,
      tls: { enable: false },
    },
    udp: {
      external_host: "127.0.0.1",
      external_port: 8990,
      listen_host: "0.0.0.0",
      listen_port: 8990,
    },
    ota: {
      test: {
        websocket: { url: "" },
        mqtt: { enable: false, endpoint: "" },
      },
      external: {
        websocket: { url: "" },
        mqtt: { enable: false, endpoint: "" },
      },
    },
    mcp: {
      global: {
        enabled: false,
        servers: [],
        reconnect_interval: 300,
        max_reconnect_attempts: 10,
      },
    },
    voice_identify: {
      enable: false,
      base_url: "",
      threshold: 0.6,
    },
    knowledge: {
      providers: {},
    },
    vision: {
      enable_auth: false,
      vision_url: "",
    },
    chat: {
      max_idle_duration: 30000,
      chat_max_silence_duration: 400,
      speak_request_reuse_window_ms: 60000,
      realtime_mode: 4,
    },
    vad: {
      provider: "ten_vad",
    },
  }
  for (const key of Object.keys(settingsSeed)) {
    const record = new Record(settings)
    record.set("key", key)
    record.set("value", settingsSeed[key])
    app.save(record)
  }
}, (app) => {
  for (const name of ["devices", "agents", "settings"]) {
    try {
      app.delete(app.findCollectionByNameOrId(name))
    } catch (_) {}
  }
  try {
    const email = $os.getenv("ADMIN_EMAIL")
    if (email) {
      app.delete(app.findAuthRecordByEmail("users", email))
    }
  } catch (_) {}
  const users = app.findCollectionByNameOrId("users")
  users.listRule = "id = @request.auth.id"
  users.viewRule = "id = @request.auth.id"
  users.createRule = ""
  users.updateRule = "id = @request.auth.id"
  users.deleteRule = "id = @request.auth.id"
  app.save(users)
})
