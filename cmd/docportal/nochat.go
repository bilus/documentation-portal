//go:build !chat

package main

import (
	"errors"

	"github.com/bilus/documentation-portal/portal"
)

// chatHandle stands for the chat in a docportal built without the tag chat,
// which holds none.
type chatHandle = struct{}

// errNoChat says that this docportal holds no chat.
var errNoChat = errors.New("this docportal is built without the chat, so -chat-model and DOCPORTAL_CHAT_MODEL do nothing here: build it with -tags chat")

// addChat returns pcfg as it is without a chat model, and refuses a chat
// model, which this docportal, built without the tag chat, cannot serve.
func addChat(pcfg portal.Config, modelID string, c chatHandle, issuer string) (portal.Config, chatHandle, error) {
	if modelID != "" {
		return portal.Config{}, c, errNoChat
	}
	return pcfg, c, nil
}
