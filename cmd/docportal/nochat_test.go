//go:build !chat

package main

import (
	"strings"
	"testing"
)

// A docportal built without the tag chat refuses a chat model at startup,
// and says how to get the chat.
func TestStartupRefusesAChatModelWithoutTheChat(t *testing.T) {
	_, _, err := startup(t.Context(), []string{"-config", sample, "-chat-model", "claude-opus-5-5"}, noEnv)
	if err == nil || !strings.Contains(err.Error(), "-tags chat") {
		t.Errorf("err = %v, want one naming the build tag", err)
	}
	if _, _, err := startup(t.Context(), []string{"-config", sample}, noEnv); err != nil {
		t.Errorf("without a chat model: %v", err)
	}
}
