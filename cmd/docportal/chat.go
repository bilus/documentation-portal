//go:build chat

package main

import (
	"net/http"

	"github.com/bilus/documentation-portal/anthropicmodel"
	"github.com/bilus/documentation-portal/chat"
	"github.com/bilus/documentation-portal/portal"
	"github.com/bilus/documentation-portal/signin"
)

// chatHandle is the chat that the builder keeps across the snapshots, or nil
// before the first build with a chat model. This file is built with the tag
// chat, which adds package chat and its private module to the program.
type chatHandle = *chat.Chat

// addChat adds the chat's routes to the portal configuration when modelID
// names a model, so that the portal handler serves a chat page in every
// portal. It builds the chat for the first snapshot, when c is nil, with the
// reader hook of the sign-in when issuer is not empty, so that a signed-in
// reader's questions count against one limit, and gives an existing chat the
// new snapshot's libraries, so that the chat keeps its conversations across
// snapshots. It returns the chat for the next snapshot: the one it built or
// was given, and on a failure the one it was given, so that a snapshot that
// fails never costs the chat its conversations. A snapshot with link portals
// alone gets no chat page, and the chat it was given keeps its conversations
// and loses its agents.
func addChat(pcfg portal.Config, modelID string, c chatHandle, issuer string) (portal.Config, chatHandle, error) {
	if modelID == "" {
		return pcfg, nil, nil
	}
	libs, err := portal.NewLibraries(pcfg)
	if err != nil {
		return portal.Config{}, c, err
	}
	// Link portals alone publish nothing to ask about, so a chat kept from
	// an earlier snapshot answers no question until a later one.
	if len(libs) == 0 {
		if c != nil {
			c.Clear()
		}
		return pcfg, c, nil
	}
	if c == nil {
		m, err := anthropicmodel.New(modelID, anthropicmodel.Config{})
		if err != nil {
			return portal.Config{}, nil, err
		}
		var reader func(*http.Request) string
		if issuer != "" {
			reader = signin.ReaderID
		}
		if c, err = chat.New(chat.Config{Model: m, Libraries: libs, Reader: reader}); err != nil {
			return portal.Config{}, nil, err
		}
	} else if err := c.Reload(libs); err != nil {
		return portal.Config{}, c, err
	}
	pcfg.Chat = c.Routes()
	return pcfg, c, nil
}
