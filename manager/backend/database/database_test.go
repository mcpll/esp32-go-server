package database

import "testing"

func TestCloseNilDatabaseDoesNotPanic(t *testing.T) {
	Close(nil)
}
