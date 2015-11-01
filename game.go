package main

import (
	"fmt"
	"time"
)

type game struct {
	opts                gameOptions
	players             []player
	round               int
	roundStart          time.Time
	gameStart           time.Time
	answerDrawPile      []answerCard
	questionDrawPile    []answerCard
	answerDiscardPile   []questionCard
	questionDiscardPile []questionCard
	messages            chan string
	done                chan struct{}
	started             bool
}

type gameOptions struct {
	awesomePointsToWin int
	minPlayers         int
	gameStarter        string
	cardBox            *cardBox
	startTimeout       time.Duration
	playerTimeout      time.Duration
	czarTimeout        time.Duration
}

type player struct {
	nick          string
	index         int
	awesomePoints int
	cards         []answerCard
}

func newGame(opts gameOptions) (*game, error) {
	err := opts.validate()
	if err != nil {
		return nil, err
	}

	game := game{
		opts:      opts,
		gameStart: time.Now(),
		messages:  make(chan string, 4), // TODO: msgs chan buffered?
		// TODO: answer/question draw pile randomization
	}

	game.join(opts.gameStarter)

	return &game, nil
}

func (o *gameOptions) validate() error {
	if o.awesomePointsToWin <= 0 {
		return fmt.Errorf("it's not a game without awesome points")
	}
	if o.minPlayers <= 2 {
		return fmt.Errorf("need 3 players to have a game")
	}
	return nil
}

func (g *game) sendMsg(msg string) {
	go func() { g.messages <- msg }()
}

func (g *game) join(nick string) {
	isPlaying := func(wantsToJoin string) bool {
		for _, p := range g.players {
			if p.nick == wantsToJoin {
				return true
			}
		}
		return false
	}

	if isPlaying(nick) {
		g.sendMsg(fmt.Sprintf("Hey %s, you're already playing, dumbass.", nick))
		return
	}

	// TODO: concurrency... trying to get something working at first
	newPlayer := player{
		nick:          nick,
		index:         len(g.players),
		awesomePoints: 0,
		cards:         nil, // TODO: give this guy some cards
	}

	g.players = append(g.players, newPlayer)

	if !g.started { // TODO atomic.LoadInt32. concurrency, baby.
		needed := g.opts.minPlayers - len(g.players) // TODO: y'know, concurrency. atomic.Add -1
		if needed > 0 {
			g.sendMsg(fmt.Sprintf("%s has joined the game! %d more players needed to start!", nick, needed))
		} else if needed == 0 {
			g.sendMsg(fmt.Sprintf("%s has joined the game! Let's start!", nick))
			g.start()
		} else {
			g.sendMsg(fmt.Sprintf("%s has joined the game!", nick))
		}
	} else {
		g.sendMsg(fmt.Sprintf("%s has joined the game!", nick))
	}

	// TODO: add to players
}

func (g *game) start() {
	// TODO: start the game, start a round, all that jazz.
}
