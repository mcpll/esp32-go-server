// Commands are the console's requests to the device server.
// The console creates them pending; the server marks them running, then done or error.

migrate((app) => {
  const user = app.findFirstRecordByFilter("users", "id != ''")
  const onlyConsole = '@request.auth.id = "' + user.id + '"'
  const agents = app.findCollectionByNameOrId("agents")
  const devices = app.findCollectionByNameOrId("devices")

  const commands = new Collection({
    type: "base",
    name: "commands",
    listRule: onlyConsole,
    viewRule: onlyConsole,
    createRule: onlyConsole,
    updateRule: onlyConsole,
    deleteRule: onlyConsole,
    fields: [
      { name: "type", type: "text", required: true, max: 64 },
      {
        name: "device",
        type: "relation",
        collectionId: devices.id,
        maxSelect: 1,
      },
      {
        name: "agent",
        type: "relation",
        collectionId: agents.id,
        maxSelect: 1,
      },
      // Not required: PocketBase treats {} as blank, and settings_reload has no payload.
      { name: "payload", type: "json" },
      {
        name: "status",
        type: "select",
        required: true,
        maxSelect: 1,
        values: ["pending", "running", "done", "error"],
      },
      { name: "result", type: "json" },
      { name: "error", type: "text", max: 8000 },
    ],
  })
  app.save(commands)
}, (app) => {
  try {
    app.delete(app.findCollectionByNameOrId("commands"))
  } catch (_) {}
})
