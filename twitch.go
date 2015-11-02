package main

import (
	"encoding/json"
	"errors"
	"io/ioutil"
	"math/rand"
	"net/http"
)

const groupClusterURL = "http://tmi.twitch.tv/servers?cluster=group"

type TwitchCluster struct {
	Cluster           string   `json:"cluster"`
	Servers           []string `json:"servers"`
	WebsocketsServers []string `json:"websockets_servers"`
}

func getWhisperServerAddress() (string, error) {
	resp, err := http.DefaultClient.Get(groupClusterURL)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	b, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	var twitchCluster TwitchCluster
	if err := json.Unmarshal(b, &twitchCluster); err != nil {
		return "", err
	}

	if len(twitchCluster.Servers) == 0 {
		return "", errors.New("no servers found in 'group' cluster")
	}

	// get random server
	n := rand.Intn(len(twitchCluster.Servers))
	return twitchCluster.Servers[n], nil
}
