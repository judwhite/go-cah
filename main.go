package main

import (
	"bufio"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"
)

var currentGame *game
var wg sync.WaitGroup // TODO: getting the game started, adding more plays. cheap hack at the moment.

func main() {
	fmt.Println("go-cah")

	cardBox, err := getCardsFromWeb()
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("questions: %d\n", len(cardBox.questions))
	fmt.Printf("answers: %d\n", len(cardBox.answers))

	wg.Add(1)
	go startTestGame(cardBox)
	wg.Wait()

	currentGame.join("player2")
	currentGame.join("player3")
	currentGame.join("player4")

	ircServerPassword := os.Getenv("TWITCH_IRC_OAUTH")
	ircNick := "judwhite"
	ircChannel := "#judwhite"

	if ircServerPassword == "" {
		log.Fatal("set $TWITCH_IRC_OAUTH")
	}
	conn, err := net.DialTimeout("tcp", "irc.twitch.tv:6667", 2*time.Second)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println("connected")

	r := bufio.NewReader(conn)

	cmds := [][]byte{
		ircGetPassCommand(ircServerPassword),
		ircGetNickCommand(ircNick),
	}
	for _, cmd := range cmds {
		//_ = cmd
		fmt.Println("cmd")
		if _, err = conn.Write(cmd); err != nil {
			log.Fatal(err)
		}
	}

	fmt.Println("reading")

	// test reading from irc server
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	line, err := r.ReadString('\n')
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(line)

	fmt.Println("read line.")

	cmds = [][]byte{
		ircGetJoinCommand(ircChannel),
		ircGetSayCommand(ircChannel, "** Hello from the CAH bot! **"),
	}
	for _, cmd := range cmds {
		//_ = cmd
		fmt.Println("cmd")
		if _, err = conn.Write(cmd); err != nil {
			log.Fatal(err)
		}
	}

	/*
	   < PASS oauth:twitch_oauth_token
	   < NICK twitch_username
	   > :tmi.twitch.tv 001 twitch_username :connected to TMI
	   > :tmi.twitch.tv 002 twitch_username :your host is TMI
	   > :tmi.twitch.tv 003 twitch_username :this server is pretty new
	   > :tmi.twitch.tv 004 twitch_username tmi.twitch.tv 0.0.1 w n
	   > :tmi.twitch.tv 375 twitch_username :- tmi.twitch.tv Message of the day -
	   > :tmi.twitch.tv 372 twitch_username :- not much to say here
	   > :tmi.twitch.tv 376 twitch_username :End of /MOTD command
	*/

	// wait for ^C
	signalChan := make(chan os.Signal, 1)
	signal.Notify(signalChan, syscall.SIGINT, syscall.SIGTERM)
	<-signalChan
}

func ircGetPassCommand(password string) []byte {
	fmt.Println("PASS **************")
	return []byte(fmt.Sprintf("PASS %s\n", password))
}

func ircGetNickCommand(nick string) []byte {
	cmd := fmt.Sprintf("NICK %s\n", nick)
	fmt.Print(cmd)
	return []byte(cmd)
}

func ircGetJoinCommand(channel string) []byte {
	cmd := fmt.Sprintf("JOIN %s\n", channel)
	fmt.Print(cmd)
	return []byte(cmd)
}

func ircGetSayCommand(target string, message string) []byte {
	cmd := fmt.Sprintf("PRIVMSG %s :%s\n", target, message)
	fmt.Print(cmd)
	return []byte(cmd)
}

func startTestGame(cardBox *cardBox) {
	var err error
	currentGame, err = newGame(gameOptions{
		awesomePointsToWin: 5,
		minPlayers:         3,
		cardBox:            cardBox,
		gameStarter:        "judwhite",
	})
	if err != nil {
		log.Fatal(err)
	}

	go listenForMessages(currentGame)
	wg.Done()
}

func listenForMessages(game *game) {
loop:
	for {
		select {
		case m := <-game.messages:
			fmt.Println(m)
		case <-game.done:
			break loop
		}
	}
}
