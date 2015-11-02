package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
)

type botConfig struct {
	nick           string
	channel        string
	serverPassword string
}

func parseArgs(args []string) (*botConfig, error) {
	flagSet := &flag.FlagSet{}
	nick := flagSet.String("nick", "", "bot's nick")
	channel := flagSet.String("channel", "", "channel to join")
	err := flagSet.Parse(args)
	if err != nil {
		return nil, err
	}

	if *nick == "" || *channel == "" {
		flagSet.PrintDefaults()
		return nil, errors.New("missing arguments")
	}

	if !strings.HasPrefix(*channel, "#") {
		*channel = fmt.Sprintf("#%s", *channel)
	}

	serverPassword := os.Getenv("TWITCH_IRC_OAUTH")
	if serverPassword == "" {
		return nil, errors.New("set TWITCH_IRC_OAUTH environment variable")
	}

	return &botConfig{
		nick:           *nick,
		channel:        *channel,
		serverPassword: serverPassword,
	}, nil
}
