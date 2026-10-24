package redis

import goredis "github.com/redis/go-redis/v9"

// SetClientForTest replaces the process Redis client. Tests use it to point
// short memory at an in-process server.
func SetClientForTest(client *goredis.Client) {
	mu.Lock()
	globalClient = client
	mu.Unlock()
}
