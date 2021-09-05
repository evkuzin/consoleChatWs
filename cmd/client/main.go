package main

import (
	"flag"
	"github.com/evkuzin/consoleChatWs/client"
	"github.com/gorilla/websocket"
	"github.com/sirupsen/logrus"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/signal"
)

var server = flag.String("server", "localhost:8080", "http service address")
var name = flag.String("name", "", "user Name")

var logger = &logrus.Logger{
	ReportCaller: true,
	Level:        logrus.InfoLevel,
	Formatter:    new(logrus.TextFormatter),
	Out:          os.Stderr,
}

func main() {

	flag.Parse()
	log.SetFlags(0)

	interrupt := make(chan os.Signal, 1)
	signal.Notify(interrupt, os.Interrupt)

	u := url.URL{Scheme: "ws", Host: *server, Path: "/ws"}
	logger.Infof("connecting to %s", u.String())

	c, _, err := websocket.DefaultDialer.Dial(u.String(), http.Header{
		"X-Api-Name": []string{*name},
	})
	if err != nil {
		logger.Debugf("dial err: %v", err)
	}
	chat := client.NewChat(c, name, logger)

	defer chat.Conn.Close()
	done := make(chan struct{})

	logger.Debug("start reading/writing messages goroutines")
	go chat.ReadPump()
	go chat.WritePump()

	logger.Debug("start reading from stdin goroutine")
	go chat.StdInScanner()

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
