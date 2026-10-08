export type JsonObject = { [key: string]: JsonValue }
export type JsonValue = string | number | boolean | null | JsonObject | JsonValue[]

export function isJsonObject(value: unknown): value is JsonObject {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
}

export function asJsonObject(value: unknown, label: string): JsonObject {
  if (!isJsonObject(value) || !isJsonValue(value)) {
    throw new Error(`${label} must be a JSON object`)
  }
  return value
}

export function isJsonValue(value: unknown): value is JsonValue {
  if (value === null) return true
  if (typeof value === 'string' || typeof value === 'number' || typeof value === 'boolean') {
    return true
  }
  if (Array.isArray(value)) return value.every(isJsonValue)
  if (isJsonObject(value)) return Object.values(value).every(isJsonValue)
  return false
}
