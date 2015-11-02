package main

import (
	"errors"
	"log"
	"strings"
)

type bot struct {
	botCfg         *botConfig
	irc            *ircClient
	game           *game
	publicMessages chan ircMessage
	exit           chan struct{}
}

func (b *bot) Start() error {
	b.publicMessages = make(chan ircMessage)
	b.exit = make(chan struct{})

	if err := b.connectIRC(); err != nil {
		return err
	}

	channel := b.irc.InitialChannels[0]
	//message := html.UnescapeString("ÇÅřʥ Ȁğāïŋşŧ ĦųΜάήίțɏ!")
	b.irc.Say(channel, "Cards Against Humanity!")
	b.irc.Say(channel, "Type !start to start a game!")

	go b.readLoop()

	return nil
}

func (b *bot) readLoop() {
loop:
	for {
		select {
		case msg := <-b.publicMessages:
			if err := b.processIRCMessage(msg); err != nil {
				log.Println(err)
			}
		case <-b.exit:
			break loop
		}
	}
}

func (b *bot) processIRCMessage(msg ircMessage) error {
	cmds := []string{"!start", "!join", "!play", "!pick", "!winner", "!gamble", "!help"}
	for _, cmd := range cmds {
		if strings.HasPrefix(msg.Message, cmd) {
			switch cmd {
			case "!start":
				if err := b.startGame(msg.Channel, msg.Nick, msg.Message); err != nil {
					return err
				}
			case "!join":
				if b.game != nil {
					b.game.join(msg.Nick)
				}
			}
		}
	}

	return nil
}

func (b *bot) startGame(channel, gameStarter, fullMessage string) error {
	if b.game != nil {
		// TODO: notify IRC
		return errors.New("game already in progress")
	}

	var err error
	if b.game, err = newGame(gameStarter, 5); err != nil {
		return err
	}
	if err = b.game.start(); err != nil {
		return err
	}

	go b.gameMessageLoop(channel, b.game.messages)

	return nil
}

func (b *bot) gameMessageLoop(channel string, messages chan string) {
loop:
	for {
		select {
		case msg := <-messages:
			b.irc.Say(channel, msg)
		case <-b.exit:
			break loop
		}
	}
}

func (b *bot) connectIRC() error {
	whisperServerAddr, err := getWhisperServerAddress()
	if err != nil {
		return err
	}

	irc := &ircClient{
		ServerAddress:         "irc.twitch.tv:6667",
		WhisperServerAddress:  whisperServerAddr,
		Nick:                  b.botCfg.nick,
		ServerPassword:        b.botCfg.serverPassword,
		InitialChannels:       []string{b.botCfg.channel},
		PublicMessageReceiver: b.publicMessages,
	}

	if err = irc.Connect(); err != nil {
		return err
	}

	b.irc = irc
	return nil
}
