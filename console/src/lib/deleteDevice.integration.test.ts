import { beforeEach, describe, expect, it, vi } from 'vitest'

const mocks = vi.hoisted(() => {
  const commandsGetFullList = vi.fn()
  const commandsDelete = vi.fn()
  const devicesDelete = vi.fn()
  const pbFilter = vi.fn((template: string, params: Record<string, string>) => {
    return `${template}@${JSON.stringify(params)}`
  })
  return { commandsGetFullList, commandsDelete, devicesDelete, pbFilter }
})

vi.mock('@/lib/client', () => ({
  pb: {
    filter: mocks.pbFilter,
    collection: (name: string) => {
      if (name === 'commands') {
        return {
          getFullList: mocks.commandsGetFullList,
          delete: mocks.commandsDelete,
        }
      }
      if (name === 'devices') {
        return { delete: mocks.devicesDelete }
      }
      throw new Error(`unexpected collection: ${name}`)
    },
  },
}))

const { commandsGetFullList, commandsDelete, devicesDelete, pbFilter } = mocks

import { deleteDevice } from '@/lib/deleteDevice'

describe('deleteDevice', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    commandsDelete.mockResolvedValue(undefined)
    devicesDelete.mockResolvedValue(undefined)
  })

  it('deletes each command for the device, then the device record', async () => {
    commandsGetFullList.mockResolvedValue([{ id: 'cmd-a' }, { id: 'cmd-b' }])

    await deleteDevice('dev-1')

    expect(pbFilter).toHaveBeenCalledWith('device = {:device}', { device: 'dev-1' })
    expect(commandsGetFullList).toHaveBeenCalledWith({
      filter: 'device = {:device}@{"device":"dev-1"}',
      fields: 'id',
    })
    expect(commandsDelete).toHaveBeenCalledTimes(2)
    expect(commandsDelete).toHaveBeenNthCalledWith(1, 'cmd-a')
    expect(commandsDelete).toHaveBeenNthCalledWith(2, 'cmd-b')
    expect(devicesDelete).toHaveBeenCalledOnce()
    expect(devicesDelete).toHaveBeenCalledWith('dev-1')

    const lastCommandDeleteOrder = commandsDelete.mock.invocationCallOrder.at(-1)
    const deviceDeleteOrder = devicesDelete.mock.invocationCallOrder[0]
    expect(lastCommandDeleteOrder).toBeLessThan(deviceDeleteOrder!)
  })

  it('deletes the device when there are no commands', async () => {
    commandsGetFullList.mockResolvedValue([])

    await deleteDevice('dev-2')

    expect(commandsDelete).not.toHaveBeenCalled()
    expect(devicesDelete).toHaveBeenCalledWith('dev-2')
  })

  it('does not delete the device when command cleanup fails', async () => {
    commandsGetFullList.mockResolvedValue([{ id: '' }])

    await expect(deleteDevice('dev-3')).rejects.toThrow(/Command id is missing/)
    expect(devicesDelete).not.toHaveBeenCalled()
  })
})
