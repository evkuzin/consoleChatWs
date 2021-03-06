package server

import (
	"encoding/json"
	"github.com/gorilla/websocket"
	"log"
	"net/http"
	"time"
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
		c.hub.Logger.Debug("Pong!")
		c.conn.SetReadDeadline(time.Now().Add(pongWait))
		return nil
	})
	msg := message{}
	for {
		t, r, err := c.conn.NextReader()
		if err != nil {
			c.hub.Logger.Error(err.Error())
			return
		}
		c.hub.Logger.Debugf("Got Message type: %v, reader: %#v", t, r)

		switch t {
		case websocket.BinaryMessage:
			c.hub.Logger.Debugf("Got binary Message from websocket")
			d := json.NewDecoder(r)
			err := d.Decode(&msg)
			if err != nil {
				c.hub.Logger.Warnf("cannot decode Message %v", err.Error())
			}
			c.hub.Logger.Debugf("Decoded Message is: %#v", msg)
		case websocket.TextMessage:
			c.hub.Logger.Debugf("Got text Message from websocket. Not implemented yet")
		}

		if msg.Command {
			switch msg.Message {
			case "/getusers":
				c.hub.Logger.Debugf("got Command get users")
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
				c.hub.Logger.Debugf("got unknown Command")
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
			c.hub.Logger.Debugf("got Message to send")
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
			c.hub.Logger.Debugf("sending Message to user %v", c.name)
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
			c.hub.Logger.Debug("Ping!")
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

// ServeWs handles websocket requests from the peer.
func ServeWs(hub *Hub, w http.ResponseWriter, r *http.Request) {
	name := r.Header.Get("X-Api-Name")
	hub.Logger.Debugf("Name: %v", name)
	if name == "" {
		http.Error(w, "Name required for auth", http.StatusProxyAuthRequired)
		hub.Logger.Debugf("Got non authenticated request from %v", r.Host)
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
		hub.Logger.Debugf("err: %v", err.Error())
		return
	}
	client := &Client{hub: hub, conn: conn, send: make(chan []byte, 256), name: name}

	// Allow collection of memory referenced by the caller by doing all work in
	// new goroutines.
	hub.Logger.Debugf("starting send/receive goroutimes for userchannel %v", name)
	go client.writePump()
	go client.readPump()
	hub.registerChan <- client
}
