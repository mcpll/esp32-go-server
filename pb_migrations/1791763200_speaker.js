// Speaker groups and samples belong to one agent. The device server enrols a
// sample by sending its wav to the voice server, then stores the uuid here.

migrate((app) => {
  const user = app.findFirstRecordByFilter("users", "id != ''")
  const onlyConsole = '@request.auth.id = "' + user.id + '"'
  const agents = app.findCollectionByNameOrId("agents")

  const groups = new Collection({
    type: "base",
    name: "speaker_groups",
    listRule: onlyConsole,
    viewRule: onlyConsole,
    createRule: onlyConsole,
    updateRule: onlyConsole,
    deleteRule: onlyConsole,
    fields: [
      {
        name: "agent",
        type: "relation",
        required: true,
        collectionId: agents.id,
        maxSelect: 1,
        cascadeDelete: true,
      },
      { name: "name", type: "text", required: true, max: 200 },
      { name: "prompt", type: "text", max: 20000 },
      { name: "voice", type: "text", max: 200 },
    ],
    indexes: [
      "CREATE UNIQUE INDEX idx_speaker_groups_agent_name ON speaker_groups (agent, name)",
    ],
  })
  app.save(groups)

  const samples = new Collection({
    type: "base",
    name: "speaker_samples",
    listRule: onlyConsole,
    viewRule: onlyConsole,
    createRule: onlyConsole,
    updateRule: onlyConsole,
    deleteRule: onlyConsole,
    fields: [
      {
        name: "group",
        type: "relation",
        required: true,
        collectionId: groups.id,
        maxSelect: 1,
        cascadeDelete: true,
      },
      {
        name: "file",
        type: "file",
        maxSelect: 1,
        maxSize: 10485760,
        mimeTypes: ["audio/wav", "audio/wave", "audio/x-wav", "audio/vnd.wave"],
      },
      { name: "uuid", type: "text", max: 80 },
      {
        name: "status",
        type: "select",
        maxSelect: 1,
        values: ["pending", "enrolled", "error"],
      },
    ],
  })
  app.save(samples)
}, (app) => {
  try {
    app.delete(app.findCollectionByNameOrId("speaker_samples"))
  } catch (_) {}
  try {
    app.delete(app.findCollectionByNameOrId("speaker_groups"))
  } catch (_) {}
})
