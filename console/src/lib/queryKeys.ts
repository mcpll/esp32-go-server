export const agentKeys = {
  all: ['agents'] as const,
  detail: (id: string) => ['agents', id] as const,
}

export const deviceKeys = {
  all: ['devices'] as const,
  screen: ['device-screen'] as const,
}

export const settingKeys = {
  all: ['settings'] as const,
}
