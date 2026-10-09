import { isJsonObject } from '@/lib/json'

export type PoolStat = {
  key: string
  kind: string
  inUse: number
  available: number
  total: number
}

export type PoolStats = {
  id: string
  pools: PoolStat[]
}

export function readPoolStats(input: unknown): PoolStats {
  const record = asRecord(input, 'Pool stats')
  const key = requiredString(record, 'key', 'Pool stats')
  if (key !== 'main') throw new Error('Pool stats key must be main')
  const id = requiredString(record, 'id', 'Pool stats')
  const data = record.data
  if (data === undefined || data === null) return { id, pools: [] }
  if (!isJsonObject(data)) throw new Error('Pool stats data must be a JSON object')
  const pools = Object.keys(data)
    .sort()
    .map((poolKey) => readPool(poolKey, data[poolKey]))
  return { id, pools }
}

export function poolStatsAfterEvent(
  current: PoolStats | null,
  event: { action: string; record: unknown },
): PoolStats | null {
  if (event.action !== 'create' && event.action !== 'update') return current
  const record = asRecord(event.record, 'Pool stats')
  if (record.key !== 'main') return current
  return readPoolStats(event.record)
}

function readPool(key: string, value: unknown): PoolStat {
  if (!isJsonObject(value)) throw new Error(`Pool ${key} must be an object`)
  const colon = key.indexOf(':')
  return {
    key,
    kind: colon > 0 ? key.slice(0, colon) : key,
    inUse: requiredNumber(value, 'in_use_resources'),
    available: requiredNumber(value, 'available_resources'),
    total: requiredNumber(value, 'total_resources'),
  }
}

function asRecord(input: unknown, label: string): { [key: string]: unknown } {
  if (!isJsonObject(input)) throw new Error(`${label} is missing`)
  return input
}

function requiredString(
  record: { [key: string]: unknown },
  key: string,
  label: string,
): string {
  const value = record[key]
  if (typeof value !== 'string' || value.trim() === '') {
    throw new Error(`${label} ${key} is missing`)
  }
  return value
}

function requiredNumber(record: { [key: string]: unknown }, key: string): number {
  const value = record[key]
  if (typeof value !== 'number' || !Number.isFinite(value)) {
    throw new Error(`${key} must be a number`)
  }
  return value
}
