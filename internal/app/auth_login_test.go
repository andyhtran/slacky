package app

import (
	"testing"
	"time"
)

func TestAuthLoginRuntimeDefaultsForProgrammaticUse(t *testing.T) {
	cmd := AuthLoginCmd{}.withRuntimeDefaults()
	if cmd.Port != defaultAuthPort {
		t.Fatalf("Port = %d, want %d", cmd.Port, defaultAuthPort)
	}
	if cmd.FallbackPort != defaultAuthFallbackPort {
		t.Fatalf("FallbackPort = %d, want %d", cmd.FallbackPort, defaultAuthFallbackPort)
	}
	if cmd.WaitTimeout != defaultAuthWaitTimeout {
		t.Fatalf("WaitTimeout = %s, want %s", cmd.WaitTimeout, defaultAuthWaitTimeout)
	}
	if cmd.WaitTimeout <= time.Second {
		t.Fatalf("WaitTimeout should not be immediate: %s", cmd.WaitTimeout)
	}
}

func TestAuthLoginRuntimeDefaultsPreserveExplicitValues(t *testing.T) {
	cmd := AuthLoginCmd{
		Port:         9998,
		FallbackPort: 9999,
		WaitTimeout:  30 * time.Second,
	}.withRuntimeDefaults()
	if cmd.Port != 9998 {
		t.Fatalf("Port = %d", cmd.Port)
	}
	if cmd.FallbackPort != 9999 {
		t.Fatalf("FallbackPort = %d", cmd.FallbackPort)
	}
	if cmd.WaitTimeout != 30*time.Second {
		t.Fatalf("WaitTimeout = %s", cmd.WaitTimeout)
	}
}

func TestAuthLoginUserSuggestionUsesResolvedIdentity(t *testing.T) {
	user := authMentionLabel("U123", "andyhtran")
	if user != "@andyhtran" {
		t.Fatalf("auth user label = %q", user)
	}
	command := defaultSearchCommand(user)
	if command != "slacky search 'from:@andyhtran has:link'" {
		t.Fatalf("default search command = %q", command)
	}
}
