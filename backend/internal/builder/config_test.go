package builder

import "testing"

func TestLoadRuntimeConfigDefaultsToLoopback(t *testing.T) {
	t.Setenv("TTP_BUILDER_ADDR", "")
	config := LoadRuntimeConfig()
	if config.Addr != "127.0.0.1:8791" {
		t.Fatalf("Addr = %q, want loopback default", config.Addr)
	}
}
