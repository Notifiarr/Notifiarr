package main

import (
	"log"

	"github.com/Go-Lift-TV/discordnotifier-client/dnclient"
)

func main() {
	if err := dnclient.Start(); err != nil {
		log.Fatal("[ERROR]", err)
	}
}
