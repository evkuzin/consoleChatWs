package client

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	term "github.com/nsf/termbox-go"
	"github.com/sirupsen/logrus"
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

type message struct {
	Name    string
	Message string
	Command bool
}

type Chat struct {
	StdIn  *bufio.Reader
	Mutex  *sync.Mutex
	Conn   *websocket.Conn
	Name   *string
	Send   chan []byte
	Logger *logrus.Logger
}

func NewChat(c *websocket.Conn, name *string, logger *logrus.Logger) *Chat {
	chat := &Chat{
		Mutex:  &sync.Mutex{},
		Conn:   c,
		Name:   name,
		Send:   make(chan []byte, 256),
		Logger: logger,
	}
	return chat
}

func (c *Chat) StdInScanner() {

	err := term.Init()
	if err != nil {
		panic(err)
	}
	defer term.Close()
	fmt.Print("Enter text: ")

	msg := make([]rune, 0)
	for {
		switch ev := term.PollEvent(); ev.Type {
		case term.EventKey:
			switch ev.Key {
			case term.KeyEnter:
				if reflect.DeepEqual(msg, []rune{}) {
					continue
				}
				fmt.Print("\rEnter text: ")

				data, err := json.Marshal(message{
					Name:    *c.Name,
					Message: string(msg),
					Command: msg[0] == '/',
				})
				if err != nil {
					c.Logger.Error(err.Error())
					return
				}
				c.Send <- data
				msg = []rune{}
				continue
			case term.KeyCtrlC:
				os.Exit(0)
			case term.KeySpace:
				fmt.Print(" ")
				msg = append(msg, ' ')
			default:
				c.Logger.Debugf("get rune %#v", ev.Ch)
				fmt.Print(string(ev.Ch))
				msg = append(msg, ev.Ch)
			}
		case term.EventError:
			panic(ev.Err)
		}

	}

}

// ReadPump pumps messages from the websocket connection to the hub.
//
// The application ensures that there is at most one reader on a connection by executing all
// reads from this goroutine.
func (c *Chat) ReadPump() {

	defer func() {
		c.Conn.Close()
	}()
	c.Conn.SetReadLimit(maxMessageSize)
	c.Conn.SetReadDeadline(time.Now().Add(pongWait))
	c.Conn.SetPongHandler(func(string) error {
		c.Conn.SetReadDeadline(time.Now().Add(pongWait))
		c.Logger.Debug("Pong!")
		return nil
	})
	msg := message{}
	for {
		t, r, err := c.Conn.NextReader()
		if err != nil {
			c.Logger.Error(err.Error())
			return
		}
		c.Logger.Debugf("Got Message type: %v, reader: %#v", t, r)

		switch t {
		case websocket.BinaryMessage:
			c.Logger.Debugf("Got binary Message from websocket")
			d := json.NewDecoder(r)
			err := d.Decode(&msg)
			if err != nil {
				c.Logger.Warnf("cannot decode Message %v", err.Error())
			}
			c.Logger.Debugf("Decoded Message is: %#v", msg)
		case websocket.TextMessage:
			c.Logger.Debugf("Got text Message from websocket")
		}
		c.logMessage(msg)
	}
}

func (c *Chat) logMessage(msg message) {
	c.Logger.Debug("push Message to stdout")
	c.Mutex.Lock()
	fmt.Printf("\r%s wrote: %s\n", msg.Name, msg.Message)
	fmt.Printf("Enter text: ")
	c.Mutex.Unlock()
}

// WritePump pumps messages from the client to the websocket connection.
//
// The application ensures that there is at most one writer to a connection by
// executing all writes from this goroutine.
func (c *Chat) WritePump() {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		c.Conn.Close()
	}()
	c.Logger.Debugf("starting writePump thread")
	for {
		select {
		case msg, ok := <-c.Send:
			c.Logger.Debugf("have Message to send: %#v", msg)
			c.Conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				// The hub closed the channel.
				c.Conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			err := c.Conn.WriteMessage(websocket.BinaryMessage, msg)
			if err != nil {
				return
			}

		case <-ticker.C:
			c.Logger.Debug("Ping!")
			c.Conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.Conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}
