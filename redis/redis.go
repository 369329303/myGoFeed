package main

import (
	"encoding/json"
	"log"
	"sort"
	"sync"
	"time"

	"github.com/369329303/myGoFeed/feed"
	"github.com/go-redis/redis"
	"github.com/mmcdole/gofeed"
)

type rssBackend struct {
	feed.UnimplementedRSSServer
}

var r *redis.Client

// UpdateDB 更新数据库
func UpdateDB(feedNames []string) {
	parser := gofeed.NewParser()
	var wg sync.WaitGroup
	var err error
	if len(feedNames) == 0 {
		if feedNames, err = r.SMembers("feeds").Result(); err != nil {
			log.Fatalf("r.SMembers failed: %v", err)
		}
	}
	for _, feedName := range feedNames {
		wg.Add(1)
		// 开启并发，加快速度
		go func(url string) {
			defer wg.Done()
			feed, err := parser.ParseURL(url)
			if err != nil {
				log.Printf("parser.ParseURL failed: %v", err)
				log.Printf("Probablely blocked by GFW.:(")
				return
			}

			// 按照从新到旧的顺序排序feed.Items
			sort.Slice(feed.Items, func(i, j int) bool {
				return feed.Items[i].PublishedParsed.After(*feed.Items[j].PublishedParsed)
			})

			// 获取数据库列表中最新的元素
			headStr, err := r.LIndex(url, 0).Result()
			if err != nil && err != redis.Nil {
				log.Fatalf("r.LIndex failed: %v", err)
			}

			if err == redis.Nil {
				// 数据库中没有这个键，直接加入
				for i := len(feed.Items) - 1; i >= 0; i-- {
					data, err := json.Marshal(feed.Items[i])
					if err != nil {
						log.Fatalf("json.Marshal failed: %v", err)
					}
					if err = r.LPush(url, data).Err(); err != nil {
						log.Fatalf("r.LPush failed: %v", err)
					}
				}
			} else {
				// 数据库中有这个键，只加入较新的
				headItem := &gofeed.Item{}
				if err = json.Unmarshal([]byte(headStr), headItem); err != nil {
					log.Fatalf("json.Unmarshal failed: %v", err)
				}

				for i := len(feed.Items) - 1; i >= 0; i-- {
					if feed.Items[i].PublishedParsed.After(*headItem.PublishedParsed) {
						data, err := json.Marshal(feed.Items[i])
						if err != nil {
							log.Fatalf("json.Marshal failed: %v", err)
						}
						if err = r.LPush(url, data).Err(); err != nil {
							log.Fatalf("r.LPush failed: %v", err)
						}
						continue
					}
				}
			}
		}(feedName)
	}
	wg.Wait()
	return
}

func main() {
	r = redis.NewClient(&redis.Options{
		Addr:     "localhost:9000",
		Password: "tEyFf2C4tXMyEZC4",
	})
	for {
		UpdateDB([]string{})
		time.Sleep(5 * time.Minute)
	}
}
