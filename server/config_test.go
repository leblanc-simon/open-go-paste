package main

import (
	"testing"

	"leblanc.io/open-go-base/appconf"
)

// usePasteConfig overrides the service settings for the duration of the test and
// restores them afterwards. The tests used to drive the code through t.Setenv,
// which no longer reaches anything: the environment is read once, at startup,
// by LoadConfig.
func usePasteConfig(t *testing.T, paste PasteConfig) {
	t.Helper()

	previous := config.Paste
	config.Paste = paste

	t.Cleanup(func() {
		config.Paste = previous
	})
}

func TestGetServerParameters(t *testing.T) {
	web := appconf.Web{Host: "127.0.0.1", Port: 8080}

	if address := getServerParameters(web); address != "127.0.0.1:8080" {
		t.Errorf("got %q, want the loopback default", address)
	}

	// An IPv6 host has to be bracketed, which the previous concatenation did not
	// do.
	web = appconf.Web{Host: "::1", Port: 9000}

	if address := getServerParameters(web); address != "[::1]:9000" {
		t.Errorf("got %q, want %q", address, "[::1]:9000")
	}
}

// OPEN_GO_PASTE_SERVER was renamed HOST by appconf.Web. Losing the old name
// silently would rebind a service listening on 0.0.0.0 to the loopback default.
func TestTheDeprecatedServerVariableStillSetsTheHost(t *testing.T) {
	t.Setenv("OPEN_GO_PASTE_SERVER", "0.0.0.0")

	// What appconf hands over is the env-default, HOST being unset.
	if host := hostFromDeprecatedEnv("127.0.0.1"); host != "0.0.0.0" {
		t.Errorf("got %q, want the deprecated variable to be honoured", host)
	}
}

func TestTheCurrentHostVariableWinsOverTheDeprecatedOne(t *testing.T) {
	t.Setenv("OPEN_GO_PASTE_SERVER", "0.0.0.0")
	t.Setenv("OPEN_GO_PASTE_HOST", "10.0.0.1")

	if host := hostFromDeprecatedEnv("10.0.0.1"); host != "10.0.0.1" {
		t.Errorf("got %q, want the value appconf resolved from HOST", host)
	}
}

func TestTheHostIsLeftAloneWithoutTheDeprecatedVariable(t *testing.T) {
	if host := hostFromDeprecatedEnv("192.168.1.1"); host != "192.168.1.1" {
		t.Errorf("got %q, want the value appconf resolved", host)
	}
}
