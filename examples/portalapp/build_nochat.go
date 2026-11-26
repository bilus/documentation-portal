//go:build !chat

package main

import (
	"log"
	"sync"

	"github.com/bilus/documentation-portal/portal"
)

// chatHandle stands for the chat in a portalapp built without the tag chat,
// which holds none.
type chatHandle = struct{}

// noChat writes its notice to the log once.
var noChat sync.Once

// addChat returns cfg as it is: this portalapp, built without the tag chat,
// serves no chat page. With an API key it says so in the log, once.
func (b *builder) addChat(cfg portal.Config) (portal.Config, error) {
	if b.model.apiKey != "" {
		noChat.Do(func() {
			log.Print("portalapp is built without the chat, so ANTHROPIC_API_KEY does nothing: build it with -tags chat")
		})
	}
	return cfg, nil
}
