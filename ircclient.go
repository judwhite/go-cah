package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type ircClient struct {
	ServerAddress        string
	WhisperServerAddress string
	Nick                 string
	ServerPassword       string
	PublicMessages       chan ircPRIVMSG
	Joins                chan ircJOIN
	Parts                chan ircPART

	// mainConn handles public messages
	conn         net.Conn
	reader       io.Reader
	lineReceiver chan string
	// whispers have to happen on the "group" cluster. we whisper a player's cards to them.
	whisperConn         net.Conn
	whisperReader       io.Reader
	whisperLineReceiver chan string

	exit      chan struct{}
	connected int32
}

type ircUserAction struct {
	Raw     string
	Channel string
	Nick    string
	User    string
	Host    string
}

type ircPRIVMSG struct {
	ircUserAction
	Message string
}

type ircJOIN struct {
	ircUserAction
}

type ircPART struct {
	ircUserAction
}

func (i *ircClient) Connect() error {
	if !atomic.CompareAndSwapInt32(&i.connected, 0, 1) {
		return errors.New("already connected")
	}

	i.lineReceiver = make(chan string)
	i.whisperLineReceiver = make(chan string)
	i.exit = make(chan struct{})

	go i.readLoop()

	var err error
	i.conn, err = net.DialTimeout("tcp", i.ServerAddress, 2*time.Second)
	if err != nil {
		return err
	}

	i.startReader(i.conn, i.lineReceiver)

	i.whisperConn, err = net.DialTimeout("tcp", i.WhisperServerAddress, 2*time.Second)
	if err != nil {
		return err
	}

	i.startReader(i.whisperConn, i.whisperLineReceiver)

	if err = i.Password(i.ServerPassword); err != nil {
		return err
	}
	if err = i.ChangeNick(i.Nick); err != nil {
		return err
	}
	if err = i.CAPREQ("twitch.tv", "membership"); err != nil {
		return err
	}

	return nil
}

func (i *ircClient) readLoop() {
loop:
	for {
		select {
		case line := <-i.lineReceiver:
			log.Printf("(M) > %s\n", line)
			i.parseLine(line, i.conn)
		case line := <-i.whisperLineReceiver:
			log.Printf("(W) > %s\n", line)
			i.tryParsePING(line, i.whisperConn)
		case <-i.exit:
			break loop
		}
	}
}

func (i *ircClient) parseLine(line string, sourceConn net.Conn) {
	i.tryParsePRIVMSG(line)
	i.tryParsePING(line, sourceConn)
	i.tryParseJOIN(line)
	i.tryParsePART(line)
}

func (i *ircClient) tryParsePING(line string, sourceConn net.Conn) {
	regex := regexp.MustCompile("^PING [:](?P<server>.+)$")
	found := regex.FindAllStringSubmatch(line, -1)
	if found == nil || len(found) != 1 || len(found[0]) != 2 {
		return
	}

	server := found[0][1]
	i.Pong(server, sourceConn)
}

func (i *ircClient) tryParsePRIVMSG(line string) {
	if i.PublicMessages == nil {
		return
	}
	regex := regexp.MustCompile(`^[:](?P<nick>.+)[!](?P<user>.+)[@](?P<host>.+) PRIVMSG (?P<channel>[#]\S+) [:](?P<msg>.+)`)
	found := regex.FindAllStringSubmatch(line, -1)
	if found == nil || len(found) != 1 || len(found[0]) != 6 {
		return
	}

	ircMsg := ircPRIVMSG{
		ircUserAction: ircUserAction{
			Raw:     line,
			Nick:    found[0][1],
			User:    found[0][2],
			Host:    found[0][3],
			Channel: found[0][4],
		},
		Message: found[0][5],
	}

	i.PublicMessages <- ircMsg
}

func (i *ircClient) tryParseJOIN(line string) {
	if i.Joins == nil {
		return
	}
	regex := regexp.MustCompile(`^[:](?P<nick>.+)[!](?P<user>.+)[@](?P<host>.+) JOIN (?P<channel>[#]\S+)`)
	found := regex.FindAllStringSubmatch(line, -1)
	if found == nil || len(found) != 1 || len(found[0]) != 5 {
		return
	}

	join := ircJOIN{
		ircUserAction: ircUserAction{
			Raw:     line,
			Nick:    found[0][1],
			User:    found[0][2],
			Host:    found[0][3],
			Channel: found[0][4],
		},
	}

	i.Joins <- join
}

func (i *ircClient) tryParsePART(line string) {
	if i.Parts == nil {
		return
	}
	regex := regexp.MustCompile(`^[:](?P<nick>.+)[!](?P<user>.+)[@](?P<host>.+) PART (?P<channel>[#]\S+)`)
	found := regex.FindAllStringSubmatch(line, -1)
	if found == nil || len(found) != 1 || len(found[0]) != 5 {
		return
	}

	part := ircPART{
		ircUserAction: ircUserAction{
			Raw:     line,
			Nick:    found[0][1],
			User:    found[0][2],
			Host:    found[0][3],
			Channel: found[0][4],
		},
	}

	i.Parts <- part
}

func (i *ircClient) startReader(conn net.Conn, receiver chan string) {
	go func() {
		r := bufio.NewReader(conn)
		for {
			line, err := r.ReadString('\n')
			if err != nil {
				//return err // TODO: send err on error channel, disconnect, retry connect
				break
			}
			line = line[:len(line)-1]
			if strings.HasSuffix(line, "\r") {
				line = line[:len(line)-1]
			}
			receiver <- line
		}
	}()
}

func (i *ircClient) CAPREQ(server, capability string) error {
	cmd := fmt.Sprintf("CAP REQ :%s/%s", server, capability)
	return i.sendCommand(cmd, i.allServers)
}

func (i *ircClient) ChangeNick(nick string) error {
	cmd := fmt.Sprintf("NICK %s", nick)
	return i.sendCommand(cmd, i.allServers)
}

func (i *ircClient) Password(password string) error {
	cmd := fmt.Sprintf("PASS %s", password)
	return i.sendCommand(cmd, i.allServers)
}

func (i *ircClient) Join(channel string) error {
	cmd := fmt.Sprintf("JOIN %s", channel)
	return i.sendCommand(cmd, i.allServers)
}

func (i *ircClient) Say(channel, message string) error {
	cmd := fmt.Sprintf("PRIVMSG %s :%s", channel, message)
	return i.sendCommand(cmd, i.mainServer)
}

func (i *ircClient) Whisper(channel, nick, message string) error {
	time.Sleep(750 * time.Millisecond) // TODO: make this async
	cmd := fmt.Sprintf("PRIVMSG %s :/w %s %s", channel, nick, message)
	return i.sendCommand(cmd, i.whisperServer)
}

func (i *ircClient) Pong(server string, conn net.Conn) error {
	cmd := fmt.Sprintf("PONG %s", server)
	return i.sendCommand(cmd, func() []net.Conn { return []net.Conn{conn} })
}

func (i *ircClient) allServers() []net.Conn {
	return []net.Conn{i.conn, i.whisperConn}
}

func (i *ircClient) mainServer() []net.Conn {
	return []net.Conn{i.conn}
}

func (i *ircClient) whisperServer() []net.Conn {
	return []net.Conn{i.whisperConn}
}

func (i *ircClient) sendCommand(cmd string, conns func() []net.Conn) error {
	// TODO: factor the logging out
	if strings.HasPrefix(cmd, "PASS") {
		log.Println("(C) < PASS **************")
	} else {
		log.Printf("(C) < %s\n", cmd)
	}

	cmd += "\n"
	byteCmd := []byte(cmd)

	var wg sync.WaitGroup
	errChan := make(chan error)

	for _, conn := range conns() {
		wg.Add(1)
		go func(c net.Conn) {
			_, err := c.Write(byteCmd)
			if err != nil {
				errChan <- err
			}
			wg.Done()
			// TODO: disconnect on write error; attempt reconnect
		}(conn)
	}

	done := make(chan struct{})

	go func() {
		wg.Wait()
		done <- struct{}{}
	}()

loop:
	for {
		select {
		case err := <-errChan:
			return err
		case <-done:
			break loop
		}
	}

	return nil
}
