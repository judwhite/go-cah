package main

import (
	"fmt"
	"log"
	"regexp"
	"strconv"
	"strings"
	"sync"
)

type bot struct {
	botCfg         *botConfig
	irc            *ircClient
	games          map[string]*game
	gamesMtx       sync.Mutex
	publicMessages chan ircPRIVMSG
	joins          chan ircJOIN
	parts          chan ircPART
	exit           chan struct{}
}

func (b *bot) Start() error {
	b.games = make(map[string]*game)
	b.publicMessages = make(chan ircPRIVMSG)
	b.joins = make(chan ircJOIN)
	b.parts = make(chan ircPART)

	b.exit = make(chan struct{})

	if err := b.connectIRC(); err != nil {
		return err
	}

	go b.readLoop()

	return nil
}

func (b *bot) readLoop() {
loop:
	for {
		select {
		case msg := <-b.publicMessages:
			if err := b.processPRIVMSG(msg); err != nil {
				log.Println(err)
			}
		case join := <-b.joins:
			b.processJOIN(join)
		case part := <-b.parts:
			b.processPART(part)
		case <-b.exit:
			break loop
		}
	}
}

func (b *bot) processPRIVMSG(msg ircPRIVMSG) error {
	cmds := []string{
		"!start",  // start new game
		"!stop",   // stop game
		"!pause",  // pause game
		"!resume", // resume game
		"!join",   // join in-progress game
		"!quit",   // quit game
		"!cards",  // show cards you have in your hand
		"!play",   // play a card or cards
		"!winner", // pick a winner (czar)
		"!points", // show players' awesome points
		"!list",   // list players in current game
		"!status", // show current status (waiting for players to play)
		"!gamble", // game an awesome point and play 2 (or 4) cards
		"!help",   // show help
		"!pick",   // same as !play or !winner
	}

	b.gamesMtx.Lock()
	game, ok := b.games[msg.Channel]
	b.gamesMtx.Unlock()

	for _, cmd := range cmds {
		if strings.HasPrefix(msg.Message, cmd) {
			switch cmd {
			case "!start":
				if err := b.startGame(msg.Channel, msg.Nick, msg.Message); err != nil {
					log.Println(err)
					return err
				}
			case "!join":
				if !ok {
					if err := b.startGame(msg.Channel, msg.Nick, msg.Message); err != nil {
						log.Println(err)
						return err
					}
				} else {
					game.join(msg.Nick)
				}
			case "!play":
				if !ok {
					b.irc.Say(msg.Channel, "No game in progress. !start to start a game")
					return nil
				}
				var nums []int
				num, err := b.extractNumber(msg.Message)
				if err != nil {
					nums, err = b.extractNumbers(msg.Message)
					if err != nil {
						log.Println(err)
						return err
					}
				} else {
					nums = []int{num}
				}
				game.play(msg.Nick, nums)
			case "!winner":
				if !ok {
					b.irc.Say(msg.Channel, "No game in progress. !start to start a game")
					return nil
				}
				num, err := b.extractNumber(msg.Message)
				if err != nil {
					log.Println(err)
					return nil
				}
				game.winner(msg.Nick, num)
			}
		}
	}

	return nil
}

func (b *bot) extractNumber(message string) (int, error) {
	regex := regexp.MustCompile("^[!]\\w+ (?P<number>\\d+)$")
	found := regex.FindAllStringSubmatch(message, -1)
	if found == nil || len(found) != 1 || len(found[0]) != 2 {
		return 0, fmt.Errorf("couldn't extract digit from %q", message)
	}

	number := found[0][1]
	return strconv.Atoi(number)
}

func (b *bot) extractNumbers(message string) ([]int, error) {
	regex := regexp.MustCompile("^[!]\\w+ (?P<number1>\\d+) (?P<number2>\\d+)$")
	found := regex.FindAllStringSubmatch(message, -1)
	if found == nil || len(found) != 1 || len(found[0]) != 3 {
		return nil, fmt.Errorf("couldn't extract 2 digits from %q", message)
	}

	number1 := found[0][1]
	num1, err := strconv.Atoi(number1)
	if err != nil {
		return nil, err
	}

	number2 := found[0][2]
	num2, err := strconv.Atoi(number2)
	if err != nil {
		return nil, err
	}

	return []int{num1, num2}, nil
}

func (b *bot) processJOIN(join ircJOIN) error {
	b.gamesMtx.Lock()
	game, ok := b.games[join.Channel]
	b.gamesMtx.Unlock()

	if !ok {
		return nil
	}

	return game.resumePlayer(join.Nick)
}

func (b *bot) processPART(part ircPART) error {
	b.gamesMtx.Lock()
	game, ok := b.games[part.Channel]
	b.gamesMtx.Unlock()

	if !ok {
		return nil
	}

	return game.suspendPlayer(part.Nick)
}

func (b *bot) startGame(channel, gameStarter, fullMessage string) error {
	b.gamesMtx.Lock()
	defer b.gamesMtx.Unlock()

	game, ok := b.games[channel]
	if ok {
		// TODO: check if user is already playing, if not add them to the current game
		b.irc.Say(channel, "Game already in progress. !join to join game")
		return nil
	}

	var err error
	if game, err = newGame(gameStarter, 5); err != nil {
		return err
	}

	b.games[channel] = game

	go b.gameMessageLoop(game, channel)

	return nil
}

func (b *bot) gameMessageLoop(game *game, channel string) {
loop:
	for {
		select {
		case msg := <-game.messages:
			b.irc.Say(channel, msg)
		case whisper := <-game.whispers:
			b.irc.Whisper(channel, whisper.nick, whisper.message)
		case <-b.exit:
			b.gamesMtx.Lock()
			delete(b.games, channel)
			b.gamesMtx.Unlock()
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
		ServerAddress:        "irc.twitch.tv:6667",
		WhisperServerAddress: whisperServerAddr,
		Nick:                 b.botCfg.nick,
		ServerPassword:       b.botCfg.serverPassword,
		PublicMessages:       b.publicMessages,
		Joins:                b.joins,
		Parts:                b.parts,
	}

	if err = irc.Connect(); err != nil {
		return err
	}

	for _, channel := range b.botCfg.channels {
		if err = irc.Join(channel); err != nil {
			// TODO: disconnect
			return err
		}
	}

	b.irc = irc
	return nil
}
