// TLS listen path moves into the mqtt_server settings record.
// The config file keeps only the broker password and signature key.

migrate((app) => {
  let record
  try {
    record = app.findFirstRecordByData("settings", "key", "mqtt_server")
  } catch (_) {
    return
  }
  const value = record.get("value") || {}
  const tls = value.tls || {}
  if (tls.port === undefined || tls.port === null || tls.port === "") tls.port = 8883
  if (!tls.pem) tls.pem = "config/server.pem"
  if (!tls.key) tls.key = "config/server.key"
  value.tls = tls
  record.set("value", value)
  app.save(record)
}, (app) => {
  let record
  try {
    record = app.findFirstRecordByData("settings", "key", "mqtt_server")
  } catch (_) {
    return
  }
  const value = record.get("value")
  if (!value || !value.tls) return
  delete value.tls.port
  delete value.tls.pem
  delete value.tls.key
  record.set("value", value)
  app.save(record)
})
