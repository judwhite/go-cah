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
	channels       []string
	serverPassword string
}

func parseArgs(args []string) (*botConfig, error) {
	flagSet := &flag.FlagSet{}
	nick := flagSet.String("nick", "", "bot's nick")
	channels := StringArray{}
	flagSet.Var(&channels, "channel", "channel to join (can be specified multiple times)")
	err := flagSet.Parse(args)
	if err != nil {
		return nil, err
	}

	if *nick == "" || len(channels) == 0 {
		flagSet.PrintDefaults()
		return nil, errors.New("missing arguments")
	}

	for i := range channels {
		if !strings.HasPrefix(channels[i], "#") {
			channels[i] = fmt.Sprintf("#%s", channels[i])
		}
	}

	serverPassword := os.Getenv("TWITCH_IRC_OAUTH")
	if serverPassword == "" {
		return nil, errors.New("set TWITCH_IRC_OAUTH environment variable")
	}

	return &botConfig{
		nick:           *nick,
		channels:       channels,
		serverPassword: serverPassword,
	}, nil
}
