package llm_memory

import "sync"

// ResetForTest drops the process-wide memory so a test can bind another Redis client.
func ResetForTest() {
	memoryInstance = nil
	once = sync.Once{}
}
