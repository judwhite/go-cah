package main

import (
	"errors"
	"fmt"
	"log"
	"math/rand"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type game struct {
	players             []*player
	playersMtx          sync.RWMutex
	gameStart           time.Time
	answerDrawPile      []answerCard
	questionDrawPile    []questionCard
	answerDiscardPile   []answerCard
	questionDiscardPile []questionCard
	messages            chan string
	whispers            chan whisper
	done                chan struct{}
	started             int32
	minStart            time.Duration
	startTimeout        time.Duration
	roundTimeout        time.Duration
	czarTimeout         time.Duration
	minPlayers          int
	awesomePointsToWin  int
	gameStarter         string
	rounds              []round
	roundsMtx           sync.RWMutex
}

type player struct {
	nick          string
	index         int
	awesomePoints int
	cards         []answerCard
}

type whisper struct {
	nick    string
	message string
}

type roundState int

const (
	RoundPlaying = iota
	RoundCzar
	RoundOver
)

type round struct {
	number   int
	state    roundState
	start    time.Time
	question questionCard
	cards    []playerAnswerCards
	players  map[string]player
	czar     string
	winner   string
}

type playerAnswerCards struct {
	nick  string
	cards []answerCard
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
		messages:           make(chan string, 10),
		whispers:           make(chan whisper, 10),
		done:               make(chan struct{}),
		minPlayers:         3,
		minStart:           30 * time.Second,
		startTimeout:       3 * time.Minute,
		czarTimeout:        2 * time.Minute,
		roundTimeout:       2 * time.Minute,
		answerDrawPile:     shuffleAnswerCards(cardBox.answers),
		questionDrawPile:   shuffleQuestionCards(cardBox.questions),
	}

	msg := fmt.Sprintf("New game has started to %d Awesome Points! Type !join to join", game.awesomePointsToWin)
	game.sendMsg(msg)

	// start nag timer
	go func() {
		ticker := time.NewTicker(60 * time.Second)
		for {
			select {
			case <-ticker.C:
				if atomic.LoadInt32(&game.started) == 0 {
					game.playersMtx.RLock()
					needed := game.minPlayers - len(game.players)
					game.playersMtx.RUnlock()
					if needed > 0 {
						game.sendMsg(fmt.Sprintf("%d more players needed to start! Type !join to join the game", needed))
					} else {
						return
					}
				}
				// TODO: select on game.exit (closed chan)
			}
		}
	}()

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
	g.messages <- msg
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

	if nick == "go_cah" {
		g.minPlayers = 2 // if the bot's playing I'm probably testing, set min players = 2
	}

	// TODO: concurrency... trying to get something working at first
	newPlayer := player{
		nick:          nick,
		index:         len(g.players),
		awesomePoints: 0,
		cards:         g.getNextAnswerCards(10),
	}

	g.players = append(g.players, &newPlayer)

	if atomic.LoadInt32(&g.started) == 0 {
		needed := g.minPlayers - len(g.players) // TODO: y'know, concurrency. atomic.Add -1
		if needed > 0 {
			g.sendMsg(fmt.Sprintf("%s has joined the game! %d more players needed to start!", nick, needed))
		} else if needed == 0 {
			// TODO: wait minimum timeout to let people join
			g.sendMsg(fmt.Sprintf("%s has joined the game! Let's start!", nick))
			err := g.start()
			if err != nil {
				// TODO: abort game?
				log.Println(err)
			}
		} else {
			g.sendMsg(fmt.Sprintf("%s has joined the game!", nick))
		}
	} else {
		// TODO: players entering an existing game should be next up for the czar position
		g.sendMsg(fmt.Sprintf("%s has joined the game!", nick))
	}
}

func (g *game) quitPlayer(nick string) error {
	// TODO
	// - if they're czar end the round without a winner. give cards back or deal new ones?
	// - if they're in the round player's list and the round.state is "playing" remove them.
	// - make sure they don't get added to the next round. remove them from the game players.
	// - publicly shame them for being scumbags. especially if they're the czar or game starter.
	g.playersMtx.Lock()
	defer g.playersMtx.Unlock()
	for i, player := range g.players {
		if player.nick == nick {
			g.players = append(g.players[:i], g.players[i+1:]...)
			g.sendMsg(fmt.Sprintf("%s has left the game. scumbag.", nick))
			round, err := g.getCurrentRound()
			if err != nil {
				return err
			}
			if round.czar == nick {
				round.cards = []playerAnswerCards{} // give players their cards back
				g.startRound()
			} else {
				if round.state == RoundPlaying {
					delete(round.players, nick)
					for i, c := range round.cards {
						if c.nick == nick {
							round.cards = append(round.cards[:i], round.cards[i+1:]...)
							break
						}
					}
					g.checkIfRoundOver(round)
				}
			}

			break
		}
	}

	return nil
}

func (g *game) suspendPlayer(nick string) error {
	// TODO: MAYBE just PART'ing will be friendlier than !quit'ing
	return g.quitPlayer(nick)
}

func (g *game) resumePlayer(nick string) error {
	// TODO
	// - y'know what, you don't get to keep your awesome points. if you left there's no resume.
	return nil
}

func (g *game) start() error {
	atomic.StoreInt32(&g.started, 1)
	err := g.startRound()
	if err != nil {
		return err
	}

	return nil
}

func (g *game) pickRandomCzar() (string, error) {
	// TODO: make sure player is still in channel
	g.playersMtx.RLock()
	defer g.playersMtx.RUnlock()

	totalPlayers := len(g.players)
	if totalPlayers == 0 {
		return "", errors.New("no players left!")
	}
	i := rand.Intn(totalPlayers)
	return g.players[i].nick, nil
}

func (g *game) pickNextCzar() (string, error) {
	round, err := g.getCurrentRound()
	if err != nil {
		// TODO: log
		return g.pickRandomCzar()
	}

	g.playersMtx.RLock()
	for i, player := range g.players {
		// TODO: make sure player is still in channel
		if player.nick == round.czar {
			if i == len(g.players)-1 {
				g.playersMtx.RUnlock()
				return g.players[0].nick, nil
			}
			g.playersMtx.RUnlock()
			return g.players[i+1].nick, nil
		}
	}
	g.playersMtx.RUnlock()

	return g.pickRandomCzar()
}

func (g *game) getCurrentRound() (*round, error) {
	g.roundsMtx.RLock()
	defer g.roundsMtx.RUnlock()

	roundIndex := len(g.rounds) - 1
	if roundIndex == -1 {
		return nil, errors.New("no current round")
	}
	return &g.rounds[roundIndex], nil
}

func (g *game) getNextAnswerCard() answerCard {
	if len(g.answerDrawPile) == 0 {
		g.answerDrawPile = shuffleAnswerCards(g.answerDiscardPile)
	}

	card := g.answerDrawPile[0]
	g.answerDrawPile = g.answerDrawPile[1:] // TODO: will this cause an empty slice if len == 1?
	g.answerDiscardPile = append(g.answerDiscardPile, card)

	return card
}

func (g *game) getNextAnswerCards(count int) []answerCard {
	var cards []answerCard
	for i := 0; i < count; i++ {
		cards = append(cards, g.getNextAnswerCard())
	}
	return cards
}

func (g *game) getNextQuestionCard() questionCard {
	if len(g.questionDrawPile) == 0 {
		g.questionDrawPile = shuffleQuestionCards(g.questionDiscardPile)
	}

	card := g.questionDrawPile[0]
	g.questionDrawPile = g.questionDrawPile[1:] // TODO: will this cause an empty slice if len == 1?
	g.questionDiscardPile = append(g.questionDiscardPile, card)

	return card
}

func (g *game) startRound() error {
	g.roundsMtx.RLock()
	roundNum := len(g.rounds) + 1
	g.roundsMtx.RUnlock()

	var err error
	var czar string
	if roundNum == 1 {
		if czar, err = g.pickRandomCzar(); err != nil {
			return err
		}
	} else {
		prevRound, err := g.getCurrentRound()
		if err != nil {
			return err
		}

		// remove played cards, deal new ones
		for _, cards := range prevRound.cards {
			for _, card := range cards.cards {
				for _, p := range g.players {
					if cards.nick == p.nick {
						for i, pcard := range p.cards {
							if pcard.ID == card.ID {
								g.answerDiscardPile = append(g.answerDiscardPile, p.cards[i])
								p.cards[i] = g.getNextAnswerCard()
							}
						}
					}
				}
			}
		}

		if czar, err = g.pickNextCzar(); err != nil {
			return err
		}
	}

	g.playersMtx.RLock()

	// if the bot is playing it will always be czar.
	for _, player := range g.players {
		if player.nick == "go_cah" {
			czar = "go_cah"
			break
		}
	}

	players := make(map[string]player)
	for _, player := range g.players {
		if player.nick == czar {
			continue
		}
		players[player.nick] = *player
	}
	g.playersMtx.RUnlock()

	r := round{
		number:   roundNum,
		start:    time.Now(),
		question: g.getNextQuestionCard(),
		players:  players,
		czar:     czar,
	}

	// TODO: round ticker, timeout on players, czar

	g.roundsMtx.Lock()
	g.rounds = append(g.rounds, r)
	g.roundsMtx.Unlock()

	g.sendMsg(fmt.Sprintf("Round %d! %s is the card czar", r.number, r.czar))
	g.sendMsg(fmt.Sprintf("QUESTION: %s", r.question.Text))

	var msgTemplate string
	if r.question.NumAnswers == 1 {
		msgTemplate = "Your cards are: %s | Type !play # to play"
	} else {
		msgTemplate = "Your cards are: %s | Type !play # # to play"
	}

	for _, player := range r.players {
		var playerCards string
		for i, c := range player.cards {
			if i != 0 {
				playerCards += " "
			}
			playerCards += fmt.Sprintf("[%d] %s", i, c.Text)
		}
		g.messagePlayer(player.nick, fmt.Sprintf(msgTemplate, playerCards))
	}

	return nil
}

func (g *game) messagePlayer(nick, message string) {
	g.whispers <- whisper{nick: nick, message: message}
}

func (g *game) play(nick string, cardIndexes []int) error {
	round, err := g.getCurrentRound()
	if err != nil {
		return err
	}

	player, ok := round.players[nick]
	if !ok {
		return fmt.Errorf("%q isn't a player in this round", nick)
	}

	if round.state != RoundPlaying {
		return errors.New("picking answers for this round is over")
	}

	if len(cardIndexes) != round.question.NumAnswers {
		plural := ""
		if round.question.NumAnswers > 1 {
			plural = "s"
		}
		g.sendMsg(fmt.Sprintf("%s, pick %d card%s", nick, round.question.NumAnswers, plural))
		return nil
	}

	var answerCards []answerCard
	for _, cardIndex := range cardIndexes {
		if cardIndex >= len(player.cards) {
			g.sendMsg(fmt.Sprintf("%s, pick a number 0-%d", nick, len(player.cards)-1))
			return nil
		}

		answerCard := player.cards[cardIndex]

		if answerCards != nil {
			for _, c := range answerCards {
				if c.ID == answerCard.ID {
					g.sendMsg(fmt.Sprintf("%s, pick two different cards", nick))
					return nil
				}
			}
		}

		answerCards = append(answerCards, answerCard)
	}

	pcards := playerAnswerCards{nick: nick, cards: answerCards}

	// allow player to change their mind on the card they played
	var found bool
	for i, c := range round.cards {
		if c.nick == nick {
			round.cards[i] = pcards
			found = true
			g.messagePlayer(nick, fmt.Sprintf("Your answer for Round %d has been changed!", round.number))
			break
		}
	}

	if !found {
		round.cards = append(round.cards, pcards)
		//g.messagePlayer(nick, fmt.Sprintf("Your answer for Round %d has been received!", round.number))
	}

	g.checkIfRoundOver(round)

	return nil
}

func (g *game) checkIfRoundOver(round *round) {
	if len(round.cards) == len(round.players) {
		// round over! show the answers
		round.state = RoundCzar
		g.sendMsg(fmt.Sprintf("Round %d! Here are the answers:", round.number))

		round.cards = g.randomize(round.cards)

		for i, v := range round.cards {
			msg := fmt.Sprintf("[%d] %s", i, round.question.Text)
			for _, c := range v.cards {
				if strings.Contains(msg, "_") {
					msg = strings.Replace(msg, "_", c.Text, 1)
				} else {
					msg += " " + c.Text
				}
			}
			g.sendMsg(msg)
		}
		g.sendMsg(fmt.Sprintf("%s, pick the winner by typing !winner #", round.czar))
	}
}

func (g *game) randomize(cards []playerAnswerCards) []playerAnswerCards {
	var shuffled []playerAnswerCards
	for _, c := range cards {
		shuffled = append(shuffled, c)
	}
	for i := 0; i < len(shuffled); i++ {
		x := rand.Intn(len(shuffled))
		shuffled[i], shuffled[x] = shuffled[x], shuffled[i]
	}

	return shuffled
}

func (g *game) winner(nick string, cardIndex int) error {
	round, err := g.getCurrentRound()
	if err != nil {
		return err
	}

	if round.czar != nick {
		return fmt.Errorf("%q is not the card czar for this round", nick)
	}

	if round.state != RoundCzar {
		return errors.New("it's not the czar's turn to pick a winner")
	}

	if cardIndex >= len(round.cards) {
		g.sendMsg(fmt.Sprintf("%s, pick a number 0-%d", nick, len(round.cards)-1))
		return nil
	}

	var gameOver bool

	round.winner = round.cards[cardIndex].nick
	round.state = RoundOver

	var winnerAwesomePoints int
	for _, player := range g.players {
		if player.nick == round.winner {
			player.awesomePoints++
			winnerAwesomePoints = player.awesomePoints
			if player.awesomePoints >= g.awesomePointsToWin {
				gameOver = true
			}
		}
	}

	if gameOver {
		g.sendMsg(fmt.Sprintf("Game Over! %s is the winner with %d Awesome Points!", round.winner, winnerAwesomePoints))
		awesomest := g.sortByAwesomePoints(g.players)
		finalStats := "Total Awesome Points: "
		for i, a := range awesomest {
			if i != 0 {
				finalStats += ", "
			}
			finalStats += fmt.Sprintf("%s: %d", a.nick, a.awesomePoints)
		}
		g.sendMsg(finalStats)

		// TODO: print overall stats
		// TODO: stop game
		close(g.done)
		return nil
	} else {
		g.sendMsg(fmt.Sprintf("%s wins this round and now has a total of %d Awesome Points!", round.winner, winnerAwesomePoints))
	}

	g.startRound()

	return nil
}

type SortablePlayers struct {
	players []*player
}

func (g *game) sortByAwesomePoints(players []*player) []*player {
	sortable := SortablePlayers{players: players}
	sort.Sort(sortable)
	sort.Reverse(sortable)
	return sortable.players
}

func (s SortablePlayers) Len() int {
	return len(s.players)
}

func (s SortablePlayers) Less(i, j int) bool {
	return s.players[i].awesomePoints < s.players[j].awesomePoints
}

func (s SortablePlayers) Swap(i, j int) {
	s.players[i], s.players[j] = s.players[j], s.players[i]
}
