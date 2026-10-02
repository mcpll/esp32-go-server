package store

import (
	"sync"
	"testing"

	"github.com/spf13/viper"
)

func TestMergeWhileReadersRun(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)

	if err := Merge(map[string]any{"ota": map[string]any{"url": "http://a"}}); err != nil {
		t.Fatal(err)
	}

	const readers = 8
	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(readers)
	for i := 0; i < readers; i++ {
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
					_ = GetString("ota.url")
					_ = GetBool("auth.enable")
					_ = GetStringMap("ota")
					_ = AllSettings()
				}
			}
		}()
	}

	for i := 0; i < 50; i++ {
		if err := Merge(map[string]any{"ota": map[string]any{"url": "http://b"}}); err != nil {
			t.Fatal(err)
		}
	}
	close(stop)
	wg.Wait()

	if got := GetString("ota.url"); got != "http://b" {
		t.Fatalf("ota.url = %q, want http://b", got)
	}
}
