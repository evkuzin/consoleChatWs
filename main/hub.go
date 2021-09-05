// Copyright 2013 The Gorilla WebSocket Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"encoding/json"
	"fmt"
)

// Hub maintains the set of active clients and broadcasts messages to the
// clients.
type Hub struct {
	// Registered clients.
	clients map[*Client]bool

	// Inbound messages from the clients.
	broadcastChan chan []byte

	// Register requests from the clients.
	registerChan chan *Client

	// Unregister requests from clients.
	unregisterChan chan *Client
}

func newHub() *Hub {
	return &Hub{
		broadcastChan:  make(chan []byte, 256),
		registerChan:   make(chan *Client, 2),
		unregisterChan: make(chan *Client, 2),
		clients:        make(map[*Client]bool),
	}
}

func (h *Hub) run() {
	for {
		select {
		case client := <-h.registerChan:
			logger.Debugf("register user: %#v", client)
			data, err := json.Marshal(message{
				Name:    "server",
				Message: fmt.Sprintf("%s joined the channel", client.name),
				Command: false,
			})
			if err != nil {
				logger.Errorf("error: %v", err)
			}
			h.clients[client] = true
			h.broadcastChan <- data
		case client := <-h.unregisterChan:
			if _, ok := h.clients[client]; ok {
				logger.Debugf("unregister user: %v", client)
				delete(h.clients, client)
				close(client.send)
			}
		case message := <-h.broadcastChan:
			logger.Debugf("broadcast Message: %s, number of users: %v", message, len(h.clients))

			for client := range h.clients {
				select {
				case client.send <- message:
					logger.Debugf("Sending message to user %v", client.name)
				default:
					logger.Debugf("closing channel for user %v", client.name)
					close(client.send)
					delete(h.clients, client)
				}
			}
		}
	}
}
