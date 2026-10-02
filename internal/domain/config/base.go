package user_config

import (
	"fmt"
	"os"
	"strings"
	"sync"

	"xiaozhi-esp32-server-golang/internal/domain/config/pocketbase"

	"github.com/spf13/viper"
)

// ProviderPocketBase is the only config_provider.type there is.
const ProviderPocketBase = "pocketbase"

var (
	pocketBaseOnce     sync.Once
	pocketBaseProvider *pocketbase.Provider
	pocketBaseErr      error
)

// GetProvider returns the config provider for config_provider.type. An empty type means pocketbase.
// The provider is a process-wide singleton: it holds the PocketBase session and the event handlers.
func GetProvider(sType string) (UserConfigProvider, error) {
	switch strings.TrimSpace(sType) {
	case "", ProviderPocketBase:
		p, err := sharedPocketBase()
		if err != nil {
			return nil, err
		}
		return p, nil
	default:
		return nil, fmt.Errorf("unsupported config provider: %s", sType)
	}
}

func sharedPocketBase() (*pocketbase.Provider, error) {
	pocketBaseOnce.Do(func() {
		url := firstSet(os.Getenv("POCKETBASE_URL"), viper.GetString("pocketbase.url"))
		email := firstSet(os.Getenv("POCKETBASE_EMAIL"), viper.GetString("pocketbase.email"))
		password := firstSet(os.Getenv("POCKETBASE_PASSWORD"), viper.GetString("pocketbase.password"))
		if url == "" || email == "" || password == "" {
			pocketBaseErr = fmt.Errorf("pocketbase.url, pocketbase.email and pocketbase.password are required (env: POCKETBASE_URL, POCKETBASE_EMAIL, POCKETBASE_PASSWORD)")
			return
		}
		pocketBaseProvider = pocketbase.NewProvider(pocketbase.NewClient(url, email, password))
	})
	return pocketBaseProvider, pocketBaseErr
}

func firstSet(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}
