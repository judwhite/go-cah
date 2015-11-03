package main

import (
	"encoding/json"
	"html"
	"io/ioutil"
	"net/http"
	"strings"
)

const cardsURL = "https://raw.githubusercontent.com/samurailink3/hangouts-against-humanity/master/source/data/cards.js"

type card struct {
	ID         int    `json:"id"`
	Text       string `json:"text"`
	NumAnswers int    `json:"numAnswers"`
}

// holds the json from the url above
type masterCard struct {
	card
	CardType  string `json:"cardType"`
	Expansion string `json:"expansion"`
}

type cardBox struct {
	questions []questionCard
	answers   []answerCard
}

type questionCard struct {
	card
}

type answerCard struct {
	card
}

func getCardsFromWeb() (*cardBox, error) {
	resp, err := http.DefaultClient.Get(cardsURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	b, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	body := string(b)
	body = strings.Replace(body, "masterCards = ", "", 1)
	body = strings.Replace(body, "\\'", "'", -1)
	body = strings.Replace(body, "\\”", "\\\"", -1)

	var masterCards []masterCard
	err = json.Unmarshal([]byte(body), &masterCards)
	if err != nil {
		return nil, err
	}

	var questions []questionCard
	var answers []answerCard
	for _, c := range masterCards {
		c.Text = html.UnescapeString(c.Text)
		c.Text = strings.Replace(c.Text, "<br>", "", -1)
		c.Text = strings.Replace(c.Text, "<b>", "", -1)
		c.Text = strings.Replace(c.Text, "</b>", "", -1)
		c.Text = strings.Replace(c.Text, "<i>", "", -1)
		c.Text = strings.Replace(c.Text, "</i>", "", -1)
		c.Text = strings.Replace(c.Text, "<u>", "", -1)
		c.Text = strings.Replace(c.Text, "</u>", "", -1)
		c.Text = strings.Replace(c.Text, "  ", " ", -1)

		switch c.CardType {
		case "Q":
			questions = append(questions, questionCard{c.card})
		case "A":
			c.card.Text = strings.TrimRight(c.card.Text, ".")
			answers = append(answers, answerCard{c.card})
		}
	}

	return &cardBox{questions: questions, answers: answers}, nil
}
