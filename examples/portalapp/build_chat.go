//go:build chat

package main

import (
	"github.com/bilus/documentation-portal/anthropicmodel"
	"github.com/bilus/documentation-portal/chat"
	"github.com/bilus/documentation-portal/portal"
	"github.com/bilus/documentation-portal/signin"
)

// chatHandle is the chat that the builder keeps across the snapshots, or nil
// before the first build with an API key. This file is built with the tag
// chat, which adds package chat and its private module to the program.
type chatHandle = *chat.Chat

// addChat adds the chat's routes to cfg with an API key: the chat with the
// reader hook of the sign-in, kept from the last snapshot and given the
// libraries of cfg before its routes. Without an API key it returns cfg as
// it is. A failure of the libraries or the chat leaves the chat unchanged.
func (b *builder) addChat(cfg portal.Config) (portal.Config, error) {
	if b.model.apiKey == "" {
		return cfg, nil
	}
	libs, err := portal.NewLibraries(cfg)
	if err != nil {
		return portal.Config{}, err
	}
	// Link portals alone publish nothing to ask about, so the chat of an
	// earlier snapshot answers no question until a later one.
	if len(libs) == 0 {
		if b.chat != nil {
			b.chat.Clear()
		}
		return cfg, nil
	}
	if b.chat == nil {
		m, err := anthropicmodel.New(b.model.model, anthropicmodel.Config{APIKey: b.model.apiKey, BaseURL: b.model.baseURL})
		if err != nil {
			return portal.Config{}, err
		}
		c, err := chat.New(chat.Config{Model: m, Libraries: libs, Reader: signin.ReaderID})
		if err != nil {
			return portal.Config{}, err
		}
		b.chat = c
	} else if err := b.chat.Reload(libs); err != nil {
		return portal.Config{}, err
	}
	// Routes binds each chat page to the libraries of this snapshot.
	cfg.Chat = b.chat.Routes()
	return cfg, nil
}
