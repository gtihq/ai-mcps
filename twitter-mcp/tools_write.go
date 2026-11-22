package main

import (
	"fmt"

	playwright "github.com/mxschmitt/playwright-go"
)

const textbox = `[data-testid="tweetTextarea_0"]`

func openTweet(tweetURL string) (playwright.Page, error) {
	page, err := openPage(tweetURL, `[data-testid="tweet"]`, []string{"TweetDetail", "TweetResultByRestId"})
	if err != nil {
		return nil, err
	}
	if page == nil {
		return nil, &TwitterError{Kind: ACCOUNT_NOT_FOUND, Detail: fmt.Sprintf("The tweet %s is not available.", tweetURL)}
	}
	return page, nil
}

func typeAndSend(page playwright.Page, text string) error {
	box := page.Locator(textbox).First()
	if err := box.Click(); err != nil {
		return err
	}
	if err := box.Fill(text); err != nil {
		return err
	}
	if err := page.Locator(`[data-testid="tweetButton"], [data-testid="tweetButtonInline"]`).First().Click(); err != nil {
		return err
	}
	page.WaitForTimeout(3000)
	return nil
}

func postTweet(text string) (string, error) {
	page, err := openPage("https://x.com/compose/post", textbox, nil)
	if err != nil {
		return "", err
	}
	if page == nil {
		return "", &TwitterError{Kind: ACCOUNT_NOT_FOUND, Detail: "The compose box did not open."}
	}
	if err := typeAndSend(page, text); err != nil {
		return "", err
	}
	return fmt.Sprintf("Tweet posted: %s...", truncateRunes(text, 50)), nil
}

func replyToTweet(tweetURL, text string) (string, error) {
	page, err := openTweet(tweetURL)
	if err != nil {
		return "", err
	}
	if err := page.Locator(`[data-testid="reply"]`).First().Click(); err != nil {
		return "", err
	}
	if _, err := page.WaitForSelector(textbox, playwright.PageWaitForSelectorOptions{Timeout: playwright.Float(10000)}); err != nil {
		return "", err
	}
	if err := typeAndSend(page, text); err != nil {
		return "", err
	}
	return fmt.Sprintf("Replied to tweet: %s...", truncateRunes(text, 50)), nil
}

func likeTweet(tweetURL string) (string, error) {
	page, err := openTweet(tweetURL)
	if err != nil {
		return "", err
	}
	if err := page.Locator(`[data-testid="like"]`).First().Click(); err != nil {
		return "", err
	}
	page.WaitForTimeout(2000)
	return fmt.Sprintf("Liked tweet: %s", tweetURL), nil
}

func retweet(tweetURL string) (string, error) {
	page, err := openTweet(tweetURL)
	if err != nil {
		return "", err
	}
	if err := page.Locator(`[data-testid="retweet"]`).First().Click(); err != nil {
		return "", err
	}
	if err := page.Locator(`[data-testid="retweetConfirm"]`).First().Click(); err != nil {
		return "", err
	}
	page.WaitForTimeout(2000)
	return fmt.Sprintf("Retweeted tweet: %s", tweetURL), nil
}
