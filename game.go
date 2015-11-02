package main

import (
	"errors"
	"fmt"
	"math/rand"
	"time"
)

type game struct {
	players             []player
	round               int
	roundStart          time.Time
	gameStart           time.Time
	answerDrawPile      []answerCard
	questionDrawPile    []questionCard
	answerDiscardPile   []answerCard
	questionDiscardPile []questionCard
	messages            chan string
	done                chan struct{}
	started             bool
	minStart            time.Duration
	startTimeout        time.Duration
	roundTimeout        time.Duration
	czarTimeout         time.Duration
	minPlayers          int
	awesomePointsToWin  int
	gameStarter         string
}

type player struct {
	nick          string
	index         int
	awesomePoints int
	cards         []answerCard
}

type round struct {
	number   int
	question questionCard
	cards    map[string][]answerCard
}

func newGame(gameStarter string, awesomePoints int) (*game, error) {
	if awesomePoints < 1 {
		// TODO: notify irc
		return nil, errors.New("need to play to at least 1 awesome point")
	}

	cardBox, err := getCardsFromWeb()
	if err != nil {
		return nil, err
	}

	game := game{
		gameStart:          time.Now(),
		gameStarter:        gameStarter,
		awesomePointsToWin: awesomePoints,
		messages:           make(chan string, 4), // TODO: msgs chan buffered?
		done:               make(chan struct{}),
		minPlayers:         2, // for testing. make this 3 eventually.
		minStart:           30 * time.Second,
		startTimeout:       3 * time.Minute,
		czarTimeout:        2 * time.Minute,
		roundTimeout:       2 * time.Minute,
		answerDrawPile:     shuffleAnswerCards(cardBox.answers),
		questionDrawPile:   shuffleQuestionCards(cardBox.questions),
	}

	msg := fmt.Sprintf("New game has started to %d Awesome Points! Type !join to join", game.awesomePointsToWin)
	game.sendMsg(msg)

	// TODO: start timeout timer

	game.join(gameStarter)

	return &game, nil
}

func shuffleAnswerCards(cards []answerCard) []answerCard {
	var shuffled []answerCard
	for _, c := range cards {
		shuffled = append(shuffled, c)
	}
	for i := 0; i < len(shuffled); i++ {
		x := rand.Intn(len(shuffled))
		shuffled[i], shuffled[x] = shuffled[x], shuffled[i]
	}

	return shuffled
}

func shuffleQuestionCards(cards []questionCard) []questionCard {
	var shuffled []questionCard
	for _, c := range cards {
		shuffled = append(shuffled, c)
	}
	for i := 0; i < len(shuffled); i++ {
		x := rand.Intn(len(shuffled))
		shuffled[i], shuffled[x] = shuffled[x], shuffled[i]
	}

	return shuffled
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
		needed := g.minPlayers - len(g.players) // TODO: y'know, concurrency. atomic.Add -1
		if needed > 0 {
			g.sendMsg(fmt.Sprintf("%s has joined the game! %d more players needed to start!", nick, needed))
		} else if needed == 0 {
			// TODO: wait minimum timeout to let people join
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

func (g *game) start() error {
	// TODO: start the game, start a round, all that jazz.
	return nil
}
