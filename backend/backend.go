package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"sync"
	"time"

	"github.com/369329303/myGoFeed/feed"
	"github.com/go-redis/redis"

	"github.com/mmcdole/gofeed"

	"google.golang.org/grpc"
)

type rssBackend struct {
	feed.UnimplementedRSSServer
}

var r *redis.Client

// 添加单个 Feed
func (rb *rssBackend) AddFeed(ctx context.Context, req *feed.Feed) (*feed.Status, error) {
	i, err := r.SAdd("feeds", req.Name).Result()
	fmt.Println(i)
	return &feed.Status{Count: i}, err
}

// 添加多个 Feed
func (rb *rssBackend) AddFeedGroup(ctx context.Context, req *feed.FeedGroup) (*feed.Status, error) {
	i, err := r.SAdd("feeds", req.FeedNames).Result()
	return &feed.Status{Count: i}, err
}

// 订阅并立即刷新 (双向流服务)
func (rb *rssBackend) SubscribeAndRefresh(srv feed.RSS_SubscribeAndRefreshServer) error {
	var wg sync.WaitGroup
	for {
		feedElem, err := srv.Recv()
		if err == io.EOF {
			wg.Wait()
			return nil
		}
		if err != nil {
			return err
		}

		wg.Add(1)
		go func() {
			defer wg.Done()
			exist, err := r.SIsMember("feeds", feedElem.Name).Result()
			if err != nil {
				if err == redis.Nil {
					r.SAdd("feeds", feedElem.Name)
				} else {
					log.Fatalf("r.SIsMember failed: %v", err)
				}
			}
			if !exist {
				UpdateDB([]string{feedElem.Name})
			}

			items, err := r.LRange(feedElem.Name, 0, 1).Result()
			if err != nil {
				log.Fatalf("r.LRange failed: %v", err)
			}
			for _, item := range items {
				rec := &gofeed.Item{}
				if err = json.Unmarshal([]byte(item), rec); err != nil {
					log.Fatalf("json.Unmarshal failed: %v", err)
				}
				story := &feed.Story{
					Title:       rec.Title,
					Link:        rec.Link,
					Description: rec.Description,
				}
				if err = srv.Send(story); err != nil {
					log.Fatalf("srv.Send failed: %v", err)
				}
			}
		}()

	}
}

// 前端服务器获取 Feed
func (rb *rssBackend) GetFeed(req *feed.Feed, srv feed.RSS_GetFeedServer) error {
	items, err := r.LRange(req.Name, req.Start, req.End).Result()
	if err != nil && err != redis.Nil {
		return err
	}

	startTime, err := time.Parse("2006-01-02 15:04:05", req.StartTime)
	if err != nil {
		return err
	}
	endTime, err := time.Parse("2006-01-02 15:04:05", req.EndTime)
	if err != nil {
		return err
	}

	for _, item := range items {
		rec := &gofeed.Item{}
		if err := json.Unmarshal([]byte(item), rec); err != nil {
			return err
		}

		if rec.PublishedParsed.After(endTime) {
			continue
		}

		if rec.PublishedParsed.Before(startTime) {
			break
		}

		story := feed.Story{
			Title:       rec.Title,
			Link:        rec.Link,
			Description: rec.Description,
		}
		if err = srv.Send(&story); err != nil {
			return err
		}
	}
	return nil
}

// 获取多个Feed
func (rb *rssBackend) GetFeedGroup(req *feed.FeedGroup, srv feed.RSS_GetFeedGroupServer) error {
	for _, name := range req.FeedNames {
		feed := feed.Feed{
			Name:      name,
			Start:     0,
			End:       req.Nums / int64(len(req.FeedNames)),
			StartTime: req.StartTime,
			EndTime:   req.EndTime,
		}
		rb.GetFeed(&feed, srv)
	}
	return nil
}

// UpdateDB 更新数据库
func UpdateDB(urls []string) {
	parser := gofeed.NewParser()
	for _, url := range urls {
		// 开启并发，加快速度
		go func(url string) {
			feed, err := parser.ParseURL(url)
			if err != nil {
				log.Fatalf("parser.ParseURL failed: %v", err)
			}

			// 获取数据库列表中最新的元素
			headStr, err := r.LIndex(url, 0).Result()
			if err != nil && err != redis.Nil {
				log.Fatalf("r.LIndex failed: %v", err)
			}

			if err == redis.Nil {
				// 数据库中没有这个键，直接加入
				for i := len(feed.Items); i >= 0; i-- {
					data, err := json.Marshal(feed.Items[i])
					if err != nil {
						log.Fatalf("json.Marshal failed: %v", err)
					}
					r.LPush(url, data)
				}
			} else {
				// 数据库中有这个键，只加入较新的
				headItem := &gofeed.Item{}
				if err = json.Unmarshal([]byte(headStr), headItem); err != nil {
					log.Fatalf("json.Unmarshal failed: %v", err)
				}

				for i := len(feed.Items); i >= 0; i-- {
					if feed.Items[i].PublishedParsed.After(*headItem.PublishedParsed) {
						data, err := json.Marshal(feed.Items[i])
						if err != nil {
							log.Fatalf("json.Marshal failed: %v", err)
						}
						r.LPush(url, data)
						continue
					}
					break
				}
			}
		}(url)
	}
}

func main() {
	r = redis.NewClient(&redis.Options{})
	lis, err := net.Listen("tcp", "localhost:8888")
	if err != nil {
		log.Fatalf("net.Listen failed: %v\n", err)
	}

	grpcServer := grpc.NewServer()
	feed.RegisterRSSServer(grpcServer, &rssBackend{})
	grpcServer.Serve(lis)
}
