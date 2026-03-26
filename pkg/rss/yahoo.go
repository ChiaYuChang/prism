package rss

import (
	"crypto/md5"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/gorilla/feeds"
)

var loc, _ = time.LoadLocation("Asia/Taipei")

type YahooNewsItem struct {
	Title     string `json:"title"`
	Summary   string `json:"summary"`
	Publisher string `json:"publisher"`
	Timestamp int64  `json:"pubtime"`
	Url       string `json:"url"`
}

func (item YahooNewsItem) PubblishAt() time.Time {
	return time.Unix(item.Timestamp/1000, (item.Timestamp%1000)*1000000).In(loc)
}

func YahooNews(category string) (string, error) {
	req, err := http.NewRequest(http.MethodGet, fmt.Sprintf("https://tw.news.yahoo.com/%s/", category), nil)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("User-Agent", `Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36`)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,image/apng,*/*;q=0.8")
	req.Header.Set("Accept-Language", "zh-TW,zh;q=0.9,en-US;q=0.8,en;q=0.7")
	req.Header.Set("Referer", "https://www.google.com/")
	req.Header.Set("Sec-Ch-Ua", `"Chromium";v="122", "Not(A:Brand";v="24", "Google Chrome";v="122"`)
	req.Header.Set("Sec-Fetch-Dest", "document")
	req.Header.Set("Sec-Fetch-Mode", "navigate")
	req.Header.Set("Sec-Fetch-Site", "cross-site")
	req.Header.Set("Upgrade-Insecure-Requests", "1")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to get page: %w", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			fmt.Println("failed to close response body", "error", err)
		}
	}()

	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("status code: %d, message: %s", resp.StatusCode, string(b))
	}

	content, _ := io.ReadAll(resp.Body)
	re := regexp.MustCompile(`(?s)"stream_items"\s*:\s*(\[.*?\])\s*,\s*"stream_total"`)
	match := re.FindSubmatch(content)

	if len(match) < 2 {
		return "", errors.New("stream_items not found")
	}

	var items []YahooNewsItem
	if err := json.Unmarshal(match[1], &items); err != nil {
		return "", fmt.Errorf("failed to unmarshal json: %w", err)
	}

	feed := feeds.Feed{
		Title:    "Yahoo News",
		Subtitle: strings.ToTitle(category),
		Link: &feeds.Link{
			Href: req.URL.String(),
		},
		Description: "RSS feed from Yahoo News",
		Created:     time.Now().In(loc),
		Items:       make([]*feeds.Item, 0, len(items)),
	}
	for _, item := range items {
		if item.Title == "" {
			continue
		}

		hasher := md5.New()
		hasher.Write([]byte(item.Url))
		feed.Items = append(feed.Items, &feeds.Item{
			Title:       item.Title,
			Id:          base64.StdEncoding.EncodeToString(hasher.Sum(nil)),
			Link:        &feeds.Link{Href: item.Url},
			Description: item.Summary,
			Author:      &feeds.Author{Name: item.Publisher},
			Created:     item.PubblishAt(),
		})
	}
	return feed.ToRss()
}
