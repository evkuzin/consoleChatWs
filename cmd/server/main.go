package main

import (
	"flag"
	"log"
	"net/http"
	"os"

	"github.com/evkuzin/consoleChatWs/server"
	"github.com/sirupsen/logrus"
)

var addr = flag.String("addr", ":8080", "http service address")
var logger = &logrus.Logger{
	ReportCaller: true,
	Level:        logrus.InfoLevel,
	Formatter:    new(logrus.TextFormatter),
	Out:          os.Stderr,
}

func serveHome(w http.ResponseWriter, r *http.Request) {
	log.Println(r.URL)
	if r.URL.Path != "/" {
		http.Error(w, "Not found", http.StatusNotFound)
		return
	}
	if r.Method != "GET" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	http.ServeFile(w, r, "home.html")
}

func main() {
	flag.Parse()
	hub := server.NewHub(logger)
	go hub.Run()
	http.HandleFunc("/", serveHome)
	http.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		server.ServeWs(hub, w, r)
	})
	err := http.ListenAndServe(*addr, nil)
	if err != nil {
		log.Fatal("ListenAndServe: ", err)
	}
}
