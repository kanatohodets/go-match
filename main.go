package main

import (
	"fmt"
	log "github.com/Sirupsen/logrus"
	"github.com/kanatohodets/go-match/matchbot"
	"os"
	"os/signal"
)

func main() {
	log.SetLevel(log.InfoLevel)

	matchbot := matchbot.New()
	go matchbot.Start("localhost:8200", "FooUser", "foobar", "blorg.json")

	// gracefully exit on SIGINT
	// (mostly, make sure the server is told to clean up queues that this bot hosted)
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt)
	<-c

	fmt.Printf("exiting ...\n")
	matchbot.Shutdown()
	os.Exit(1)
}
