package main

import (
	"bytes"
	"encoding/json"
	"strings"
)

const notificationsJS = `() => [...document.querySelectorAll('[data-testid="cellInnerDiv"]')].slice(0, 20).map(el => ({
    text: el.innerText, timestamp: (el.querySelector('time') || {getAttribute: () => ''}).getAttribute('datetime') || ''
}))`

type jsonKV struct {
	key string
	val string
}

// pyJSONArray reproduces json.dumps(..., ensure_ascii=False) for arrays of
// string objects: a comma plus space between items and a colon plus space
// between keys and values. Go's json.Marshal would omit those spaces.
func pyJSONArray(rows [][]jsonKV) string {
	var b strings.Builder
	b.WriteByte('[')
	for i, row := range rows {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteByte('{')
		for j, field := range row {
			if j > 0 {
				b.WriteString(", ")
			}
			b.WriteString(pyJSONString(field.key))
			b.WriteString(": ")
			b.WriteString(pyJSONString(field.val))
		}
		b.WriteByte('}')
	}
	b.WriteByte(']')
	return b.String()
}

// pyJSONString quotes a string the way Python does with ensure_ascii=False.
// SetEscapeHTML(false) keeps <, > and & literal.
func pyJSONString(s string) string {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(s); err != nil {
		return `""`
	}
	return strings.TrimSuffix(buf.String(), "\n")
}

func readFeed(maxResults int) (string, error) {
	page, err := openPage("https://x.com/home", `[data-testid="tweet"]`, []string{"HomeTimeline", "HomeLatestTimeline"})
	if err != nil {
		return "", err
	}
	if page == nil {
		return "[]", nil
	}
	tweets, err := collectTweets(page, maxResults, 10)
	if err != nil {
		return "", err
	}
	rows := make([][]jsonKV, 0, len(tweets))
	for _, t := range tweets {
		rows = append(rows, []jsonKV{
			{key: "author", val: t.Name},
			{key: "text", val: t.Text},
			{key: "timestamp", val: t.Time},
		})
	}
	return pyJSONArray(rows), nil
}

func readNotifications() (string, error) {
	page, err := openPage(
		"https://x.com/notifications",
		`[data-testid="cellInnerDiv"] time, [data-testid="notification"]`,
		[]string{"NotificationsTimeline"},
	)
	if err != nil {
		return "", err
	}
	if page == nil {
		return "[]", nil
	}
	value, err := page.Evaluate(notificationsJS)
	if err != nil {
		return "", err
	}
	items := []struct {
		Text      string `json:"text"`
		Timestamp string `json:"timestamp"`
	}{}
	if err := decodeValue(value, &items); err != nil {
		return "", err
	}
	notifications := [][]jsonKV{}
	for _, item := range items {
		lines := []string{}
		for _, raw := range strings.Split(item.Text, "\n") {
			if line := strings.TrimSpace(raw); line != "" {
				lines = append(lines, line)
			}
		}
		if len(lines) > 5 {
			lines = lines[:5]
		}
		text := strings.Join(lines, " ")
		if len([]rune(text)) >= 5 {
			notifications = append(notifications, []jsonKV{
				{key: "text", val: truncateRunes(text, 300)},
				{key: "timestamp", val: item.Timestamp},
			})
		}
	}
	return pyJSONArray(notifications), nil
}

func readBookmarks(maxResults int) (string, error) {
	page, err := openPage("https://x.com/i/bookmarks", `[data-testid="tweet"]`, []string{"Bookmarks"})
	if err != nil {
		return "", err
	}
	if page == nil {
		return "[]", nil
	}
	tweets, err := collectTweets(page, maxResults, 10)
	if err != nil {
		return "", err
	}
	rows := make([][]jsonKV, 0, len(tweets))
	for _, t := range tweets {
		link := t.Href
		if strings.HasPrefix(link, "/") {
			link = "https://x.com" + link
		}
		rows = append(rows, []jsonKV{
			{key: "author", val: t.Name},
			{key: "text", val: t.Text},
			{key: "timestamp", val: t.Time},
			{key: "link", val: link},
		})
	}
	return pyJSONArray(rows), nil
}
