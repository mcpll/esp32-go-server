// One pool_stats record (key "main"). The device server overwrites data every 5s
// when pool_stats.report_enabled is true, and sends nothing when it is false.

migrate((app) => {
  const users = app.findRecordsByFilter("users", "id != ''", "created", 1, 0)
  if (users.length === 0) {
    throw new Error("pool_stats migration needs the console user")
  }
  const onlyConsole = '@request.auth.id = "' + users[0].id + '"'

  const poolStats = new Collection({
    type: "base",
    name: "pool_stats",
    listRule: onlyConsole,
    viewRule: onlyConsole,
    createRule: onlyConsole,
    updateRule: onlyConsole,
    deleteRule: onlyConsole,
    fields: [
      { name: "key", type: "text", required: true, max: 64 },
      // Not required: the JSVM treats an empty object as blank, and the reporter
      // writes the first object (including {}) over REST.
      { name: "data", type: "json" },
    ],
    indexes: [
      "CREATE UNIQUE INDEX idx_pool_stats_key ON pool_stats (key)",
    ],
  })
  app.save(poolStats)

  const record = new Record(poolStats)
  record.set("key", "main")
  app.save(record)
}, (app) => {
  try {
    app.delete(app.findCollectionByNameOrId("pool_stats"))
  } catch (_) {}
})
