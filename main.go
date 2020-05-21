package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sort"
	"sync"
	"time"

	"github.com/go-redis/redis"

	"github.com/mmcdole/gofeed"
)

// GetFeeds 获取Feeds
func GetFeeds(rClient *redis.Client, urls []string) {
	feedParser := gofeed.NewParser()
	for _, url := range urls {
		go func(url string) {
			feed, err := feedParser.ParseURL(url)
			if err != nil {
				log.Fatalln(err)
			}
			for _, item := range feed.Items {
				go func(item *gofeed.Item) {
					data, err := json.Marshal(item)
					if err != nil {
						log.Fatalln(err)
					}
					baseTime, err := time.Parse(time.RFC1123Z, "Mon, 02 Jan 2006 15:04:05 -0700")
					if err != nil {
						log.Println(err)
					}
					elapsed := item.PublishedParsed.Sub(baseTime)
					z := redis.Z{Score: elapsed.Seconds(), Member: data}

					err = rClient.ZAddNX(url, z).Err()
					if err != nil {
						r, err := rClient.ZRangeByScore(url, redis.ZRangeBy{Min: "-inf", Max: "+inf", Offset: 0, Count: 10}).Result()
						if err == nil {
							log.Fatalln(err)
						}
						fmt.Println(r)
						fmt.Println(elapsed.Hours())
						log.Fatalln(err, "------")
					}
				}(item)
			}
		}(url)
	}
}

// ReadFeeds 从Redis数据库中取出Feeds
func ReadFeeds(rClient *redis.Client, urls []string) string {
	html := `<table>
	<tr>
		<th>标题</th>
		<th>描述</th>
	</tr>
	<style>
	table {
		font-family: arial, sans-serif;
		border-collapse: collapse;
		width: 100%;
	}

	td, th {
		border: 1px solid #dddddd;
		text-align: left;
		padding: 8px;
	}

	tr:nth-child(even) {
		background-color: #dddddd;
	}
	</style>
	`
	var wg sync.WaitGroup
	var stories []string
	for _, url := range urls {
		wg.Add(1)
		go func(url string) {
			defer wg.Done()
			// 从每个Feed中取出最新的10条消息
			zs, err := rClient.ZRevRangeByScore(url, redis.ZRangeBy{
				Min:    "-inf",
				Max:    "+inf",
				Offset: 0,
				Count:  10,
			}).Result()
			if err != nil {
				log.Println(err)
			}
			stories = append(stories, zs...)

		}(url)
	}
	wg.Wait()

	var storyI, storyJ gofeed.Feed
	// 対合并的Feeds按照时间进行排序
	sort.Slice(stories, func(i, j int) bool {
		err := json.Unmarshal([]byte(stories[i]), &storyI)
		if err != nil {
			log.Fatalln(err)
		}
		err = json.Unmarshal([]byte(stories[j]), &storyJ)
		if err != nil {
			log.Fatalln(err)
		}
		if storyI.PublishedParsed.After(*storyJ.PublishedParsed) {
			return true
		}
		return false
	})

	for _, story := range stories {
		var row string

		var item gofeed.Feed
		err := json.Unmarshal([]byte(story), &item)
		if err != nil {
			log.Fatalln(err)
		}
		row += fmt.Sprintf("<tr><td><a href='%v'>%v</a></td><td>%v</td></tr>", item.Link, item.Title, item.Description)
		html += row
	}
	html += `</table>`
	return html
}

// RSSReader 阅读服务
func RSSReader(w http.ResponseWriter, req *http.Request) {
	html := ReadFeeds(rClient, urls)
	w.Write([]byte(html))
}

var rClient *redis.Client
var urls []string

func main() {
	rClient = redis.NewClient(&redis.Options{})
	urls = []string{
		"https://rss.nytimes.com/services/xml/rss/nyt/HomePage.xml",
		"https://feeds.bbci.co.uk/news/rss.xml",
	}

	go func() {
		for {
			GetFeeds(rClient, urls)
			// 每隔5min更新一次数据库
			time.Sleep(5 * time.Minute)
		}
	}()

	http.HandleFunc("/", RSSReader)
	http.ListenAndServe(":8888", nil)
}
