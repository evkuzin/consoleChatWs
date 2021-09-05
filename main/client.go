// Copyright 2013 The Gorilla WebSocket Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"encoding/json"
	"log"
	"net/http"
	"time"

	"github.com/gorilla/websocket"
)

const (
	// Time allowed to write a message to the peer.
	writeWait = 10 * time.Second

	// Time allowed to read the next pong message from the peer.
	pongWait = 60 * time.Second

	// Send pings to peer with this period. Must be less than pongWait.
	pingPeriod = (pongWait * 9) / 10

	// Maximum message size allowed from peer.
	maxMessageSize = 512
)

var (
	newline = []byte{'\n'}
	space   = []byte{' '}
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
}

type message struct {
	Name    string
	Message string
	Command bool
}

// Client is a middleman between the websocket connection and the hub.
type Client struct {
	hub *Hub

	// The websocket connection.
	conn *websocket.Conn

	// Buffered channel of outbound messages.
	send chan []byte
	name string
}

// readPump pumps messages from the websocket connection to the hub.
//
// The application runs readPump in a per-connection goroutine. The application
// ensures that there is at most one reader on a connection by executing all
// reads from this goroutine.
func (c *Client) readPump() {
	defer func() {
		c.hub.unregisterChan <- c
		c.conn.Close()
	}()
	c.conn.SetReadLimit(maxMessageSize)
	c.conn.SetReadDeadline(time.Now().Add(pongWait))
	c.conn.SetPongHandler(func(string) error {
		logger.Debug("Pong!")
		c.conn.SetReadDeadline(time.Now().Add(pongWait))
		return nil
	})
	msg := message{}
	for {
		t, r, err := c.conn.NextReader()
		if err != nil {
			logger.Error(err.Error())
			return
		}
		logger.Debugf("Got Message type: %v, reader: %#v", t, r)

		switch t {
		case websocket.BinaryMessage:
			logger.Debugf("Got binary Message from websocket")
			d := json.NewDecoder(r)
			err := d.Decode(&msg)
			if err != nil {
				logger.Warnf("cannot decode Message %v", err.Error())
			}
			logger.Debugf("Decoded Message is: %#v", msg)
		case websocket.TextMessage:
			logger.Debugf("Got text Message from websocket. Not implemented yet")
		}

		if msg.Command {
			switch msg.Message {
			case "/getusers":
				logger.Debugf("got Command get users")
				usersOnline := "Users online:\n"
				for user := range c.hub.clients {
					usersOnline += user.name + "\n"
				}
				data, err := json.Marshal(message{
					Name:    "server",
					Message: usersOnline,
					Command: false,
				})
				if err != nil {
					log.Printf("error: %v", err)
				}
				c.send <- data
			default:
				logger.Debugf("got unknown Command")
				data, err := json.Marshal(message{
					Name:    "server",
					Message: "no such Command on the server",
					Command: false,
				})
				if err != nil {
					log.Printf("error: %v", err)
				}
				c.send <- data
			}

		} else {
			logger.Debugf("got Message to send")
			data, err := json.Marshal(message{
				Name:    c.name,
				Message: msg.Message,
				Command: false,
			})
			if err != nil {
				log.Printf("error: %v", err)
			}
			c.hub.broadcastChan <- data
		}

	}
}

// writePump pumps messages from the hub to the websocket connection.
//
// A goroutine running writePump is started for each connection. The
// application ensures that there is at most one writer to a connection by
// executing all writes from this goroutine.
func (c *Client) writePump() {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		c.conn.Close()
	}()
	for {
		select {
		case message, ok := <-c.send:
			logger.Debugf("sending Message to user %v", c.name)
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				// The hub closed the channel.
				c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			err := c.conn.WriteMessage(websocket.BinaryMessage, message)
			if err != nil {
				return
			}

		case <-ticker.C:
			logger.Debug("Ping!")
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

// serveWs handles websocket requests from the peer.
func serveWs(hub *Hub, w http.ResponseWriter, r *http.Request) {
	name := r.Header.Get("X-Api-Name")
	logger.Debugf("Name: %v", name)
	if name == "" {
		http.Error(w, "Name required for auth", http.StatusProxyAuthRequired)
		logger.Debugf("Got non authenticated request from %v", r.Host)
		return
	}
	for u := range hub.clients {
		if u.name == name {
			http.Error(w, "Name is already occupied", http.StatusProxyAuthRequired)
			return
		}
	}
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		logger.Debugf("err: %v", err.Error())
		return
	}
	client := &Client{hub: hub, conn: conn, send: make(chan []byte, 256), name: name}

	// Allow collection of memory referenced by the caller by doing all work in
	// new goroutines.
	logger.Debugf("starting send/receive goroutimes for userchannel %v", name)
	go client.writePump()
	go client.readPump()
	hub.registerChan <- client
}
