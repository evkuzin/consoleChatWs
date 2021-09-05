package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"github.com/gorilla/websocket"
	term "github.com/nsf/termbox-go"
	"github.com/sirupsen/logrus"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"reflect"
	"sync"
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

var server = flag.String("server", "localhost:8080", "http service address")
var name = flag.String("name", "", "user Name")

var logger = &logrus.Logger{
	ReportCaller: true,
	Level:        logrus.InfoLevel,
	Formatter:    new(logrus.TextFormatter),
	Out:          os.Stderr,
}

type message struct {
	Name    string
	Message string
	Command bool
}

type Chat struct {
	stdIn *bufio.Reader
	mutex *sync.Mutex
	conn  *websocket.Conn
	name  *string
	send  chan []byte
}

func NewChat(c *websocket.Conn, name *string) *Chat {
	chat := &Chat{
		mutex: &sync.Mutex{},
		conn:  c,
		name:  name,
		send:  make(chan []byte, 256),
	}
	logger.Debug("Create new chat")
	return chat
}

func main() {

	flag.Parse()
	log.SetFlags(0)

	interrupt := make(chan os.Signal, 1)
	signal.Notify(interrupt, os.Interrupt)

	u := url.URL{Scheme: "ws", Host: *server, Path: "/ws"}
	log.Printf("connecting to %s", u.String())

	c, _, err := websocket.DefaultDialer.Dial(u.String(), http.Header{
		"X-Api-Name": []string{*name},
	})
	if err != nil {
		logger.Debugf("dial err: %v", err)
	}
	chat := NewChat(c, name)

	defer chat.conn.Close()
	done := make(chan struct{})

	logger.Debug("start reading/writing messages goroutines")
	go chat.readPump()
	go chat.writePump()

	logger.Debug("start reading from stdin goroutine")
	go chat.stdInScanner()

	for {
		select {
		case <-done:
			logger.Info("Closing chat")
			return
		case <-interrupt:
			logger.Debug("Closing chat")

			// Cleanly close the connection by sending a close Message and then
			// waiting (with timeout) for the server to close the connection.
			err := c.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
			if err != nil {
				logger.Error(err.Error())
			}
			return
		}
	}
}

func (c *Chat) stdInScanner() {

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
				logger.Debugf("Read from stdout: %s", string(msg))

				data, err := json.Marshal(message{
					Name:    *c.name,
					Message: string(msg),
					Command: msg[0] == '/',
				})
				if err != nil {
					logger.Error(err.Error())
					return
				}
				c.send <- data
				msg = []rune{}
				continue
			case term.KeyCtrlC:
				os.Exit(0)
			case term.KeySpace:
				fmt.Print(" ")
				msg = append(msg, ' ')
			default:
				logger.Debugf("get rune %#v", ev.Ch)
				fmt.Print(string(ev.Ch))
				msg = append(msg, ev.Ch)
			}
		case term.EventError:
			panic(ev.Err)
		}

	}

}

// readPump pumps messages from the websocket connection to the hub.
//
// The application ensures that there is at most one reader on a connection by executing all
// reads from this goroutine.
func (c *Chat) readPump() {

	defer func() {
		c.conn.Close()
	}()
	c.conn.SetReadLimit(maxMessageSize)
	c.conn.SetReadDeadline(time.Now().Add(pongWait))
	c.conn.SetPongHandler(func(string) error {
		c.conn.SetReadDeadline(time.Now().Add(pongWait))
		logger.Debug("Pong!")
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
			logger.Debugf("Got text Message from websocket")
		}
		c.logMessage(msg)
	}
}

func (c *Chat) logMessage(msg message) {
	logger.Debug("push Message to stdout")
	c.mutex.Lock()
	fmt.Printf("\r%s wrote: %s\n", msg.Name, msg.Message)
	fmt.Printf("Enter text: ")
	c.mutex.Unlock()
}

// writePump pumps messages from the client to the websocket connection.
//
// The application ensures that there is at most one writer to a connection by
// executing all writes from this goroutine.
func (c *Chat) writePump() {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		c.conn.Close()
	}()
	logger.Debugf("starting writePump thread")
	for {
		select {
		case msg, ok := <-c.send:
			logger.Debugf("have Message to send: %#v", msg)
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				// The hub closed the channel.
				c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			err := c.conn.WriteMessage(websocket.BinaryMessage, msg)
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
