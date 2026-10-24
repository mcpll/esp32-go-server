// The console cannot change the Redis switch. The device server writes it.

migrate((app) => {
  const settings = app.findCollectionByNameOrId("settings")
  settings.updateRule = lockRule(settings.updateRule, 'key != "redis_available" && @request.body.key != "redis_available"')
  settings.deleteRule = lockRule(settings.deleteRule, 'key != "redis_available"')
  settings.createRule = lockRule(settings.createRule, '@request.body.key != "redis_available"')
  app.save(settings)
}, (app) => {
  const settings = app.findCollectionByNameOrId("settings")
  settings.updateRule = unlockRule(settings.updateRule, 'key != "redis_available" && @request.body.key != "redis_available"')
  settings.deleteRule = unlockRule(settings.deleteRule, 'key != "redis_available"')
  settings.createRule = unlockRule(settings.createRule, '@request.body.key != "redis_available"')
  app.save(settings)
})

function lockRule(current, extra) {
  if (!current) return extra
  if (current.indexOf("redis_available") !== -1) return current
  return current + " && " + extra
}

function unlockRule(current, extra) {
  if (!current) return current
  return current.split(" && " + extra).join("")
}
