package secrets

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestInstallDocsListEnvAndBinding(t *testing.T) {
	root := repoRoot(t)
	readme := readRepoFile(t, filepath.Join(root, "README.md"))
	example := readRepoFile(t, filepath.Join(root, ".env.example"))

	required := []string{
		"ADMIN_EMAIL",
		"ADMIN_PASSWORD",
		"WEBSOCKET_TOKEN",
		"ENDPOINT_AUTH_TOKEN",
		"MQTT_SERVER_PASSWORD",
		"VISION_TOKEN",
		"DASHSCOPE_API_KEY",
	}
	optional := []string{
		"MQTT_SERVER_SIGNATURE_KEY",
		"MEM0_API_KEY",
		"REDIS_ENABLE",
		"REDIS_HOST",
		"REDIS_PASSWORD",
		"POCKETBASE_URL",
		"POCKETBASE_EMAIL",
		"POCKETBASE_PASSWORD",
	}
	for _, name := range append(append([]string{}, required...), optional...) {
		if !strings.Contains(readme, name) {
			t.Errorf("README.md does not mention %s", name)
		}
	}
	for _, name := range required {
		if !strings.Contains(example, name+"=") {
			t.Errorf(".env.example does not set %s", name)
		}
	}
	for _, phrase := range []string{"six-digit", "Add device", "activated"} {
		if !strings.Contains(readme, phrase) {
			t.Errorf("README.md does not describe binding (%q)", phrase)
		}
	}
}

func TestComposeRunsPocketBaseAndServerWithOptionalProfiles(t *testing.T) {
	body := readRepoFile(t, filepath.Join(repoRoot(t), "docker-compose.yml"))
	for _, snippet := range []string{
		"\n  pocketbase:\n",
		"\n  server:\n",
		"\n  redis:\n",
		"profiles: [\"redis\"]",
		"\n  qdrant:\n",
		"\n  voice-server:\n",
		"profiles: [\"voice\"]",
	} {
		if !strings.Contains(body, snippet) {
			t.Errorf("docker-compose.yml missing %q", snippet)
		}
	}
	pocketbase := section(body, "\n  pocketbase:\n", "\n  server:\n")
	server := section(body, "\n  server:\n", "\n  redis:\n")
	if strings.Contains(pocketbase, "profiles:") || strings.Contains(server, "profiles:") {
		t.Fatal("pocketbase and server must start without a profile")
	}
}

func repoRoot(t *testing.T) string {
	t.Helper()
	_, this, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(this), "..", "..", "..", "..")
}

func readRepoFile(t *testing.T, path string) string {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(body)
}

func section(body, start, end string) string {
	i := strings.Index(body, start)
	if i < 0 {
		return ""
	}
	rest := body[i+len(start):]
	j := strings.Index(rest, end)
	if j < 0 {
		return rest
	}
	return rest[:j]
}
