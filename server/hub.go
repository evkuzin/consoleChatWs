package server

import (
	"encoding/json"
	"fmt"
	"github.com/sirupsen/logrus"
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
	Logger         *logrus.Logger
}

// NewHub create a new server instance for websockets connections
func NewHub(logger *logrus.Logger) *Hub {
	return &Hub{
		broadcastChan:  make(chan []byte, 256),
		registerChan:   make(chan *Client, 2),
		unregisterChan: make(chan *Client, 2),
		clients:        make(map[*Client]bool),
		Logger:         logger,
	}
}

func (h *Hub) Run() {
	for {
		select {
		case client := <-h.registerChan:
			h.Logger.Debugf("register user: %#v", client)
			data, err := json.Marshal(message{
				Name:    "server",
				Message: fmt.Sprintf("%s joined the channel", client.name),
				Command: false,
			})
			if err != nil {
				h.Logger.Errorf("error: %v", err)
			}
			h.clients[client] = true
			h.broadcastChan <- data
		case client := <-h.unregisterChan:
			if _, ok := h.clients[client]; ok {
				h.Logger.Debugf("unregister user: %v", client)
				delete(h.clients, client)
				close(client.send)
			}
		case message := <-h.broadcastChan:
			h.Logger.Debugf("broadcast Message: %s, number of users: %v", message, len(h.clients))

			for client := range h.clients {
				select {
				case client.send <- message:
					h.Logger.Debugf("Sending message to user %v", client.name)
				default:
					h.Logger.Debugf("closing channel for user %v", client.name)
					close(client.send)
					delete(h.clients, client)
				}
			}
		}
	}
}
