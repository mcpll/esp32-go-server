// Knowledge bases and their documents. An agent links the bases it may search.
// The device server uploads and refreshes them through commands.

migrate((app) => {
  const user = app.findFirstRecordByFilter("users", "id != ''")
  const onlyConsole = '@request.auth.id = "' + user.id + '"'

  const bases = new Collection({
    type: "base",
    name: "knowledge_bases",
    listRule: onlyConsole,
    viewRule: onlyConsole,
    createRule: onlyConsole,
    updateRule: onlyConsole,
    deleteRule: onlyConsole,
    fields: [
      { name: "name", type: "text", required: true, max: 200 },
      { name: "description", type: "text", max: 2000 },
      {
        name: "provider",
        type: "select",
        required: true,
        maxSelect: 1,
        values: ["ragflow", "dify", "weknora"],
      },
      { name: "external_kb_id", type: "text", max: 200 },
      { name: "retrieval_threshold", type: "number" },
      { name: "status", type: "text", max: 32 },
    ],
  })
  app.save(bases)

  const documents = new Collection({
    type: "base",
    name: "knowledge_documents",
    listRule: onlyConsole,
    viewRule: onlyConsole,
    createRule: onlyConsole,
    updateRule: onlyConsole,
    deleteRule: onlyConsole,
    fields: [
      {
        name: "knowledge_base",
        type: "relation",
        collectionId: bases.id,
        required: true,
        maxSelect: 1,
        cascadeDelete: true,
      },
      { name: "file", type: "file", maxSelect: 1, maxSize: 52428800 },
      {
        name: "status",
        type: "select",
        required: true,
        maxSelect: 1,
        values: ["pending", "synced", "error"],
      },
      { name: "external_doc_id", type: "text", max: 200 },
    ],
  })
  app.save(documents)

  const agents = app.findCollectionByNameOrId("agents")
  agents.fields.add(
    new RelationField({
      name: "knowledge_bases",
      collectionId: bases.id,
      maxSelect: 99,
      cascadeDelete: false,
    }),
  )
  app.save(agents)
}, (app) => {
  const agents = app.findCollectionByNameOrId("agents")
  agents.fields.removeByName("knowledge_bases")
  app.save(agents)
  try {
    app.delete(app.findCollectionByNameOrId("knowledge_documents"))
  } catch (_) {}
  try {
    app.delete(app.findCollectionByNameOrId("knowledge_bases"))
  } catch (_) {}
})
