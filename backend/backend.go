package main

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"net"
	"sort"
	"strings"
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

const (
	layout = "2006-01-02T15:04"
)

// 获取所有的Feed名称 （服务端流式RPC）
func (rb *rssBackend) GetAllFeedNames(req *feed.OK, srv feed.RSS_GetAllFeedNamesServer) error {
	feedNames, err := r.SMembers("feeds").Result()
	if err == redis.Nil {
		return nil
	}
	if err != nil {
		return err
	}
	for _, feedName := range feedNames {
		feedElem := feed.Feed{
			Name: feedName,
		}
		if err = srv.Send(&feedElem); err != nil {
			return err
		}
	}
	return nil
}

// 删除多个Feeds （客户端流式RPC）
func (rb *rssBackend) DeleteFeeds(srv feed.RSS_DeleteFeedsServer) error {
	var feedNames []string
	for {
		feedElem, err := srv.Recv()
		if err == io.EOF {
			count, err := r.SRem("feeds", feedNames).Result()
			if err != nil {
				log.Fatalf("r.SRem failed: %v", err)
			}
			if err = r.Del(feedNames...).Err(); err != nil {
				log.Fatalf("r.Del failed: %v", err)
			}
			err = srv.SendAndClose(&feed.Status{Count: count})
			if err != nil {
				log.Fatalf("srv.SendAndClose failed: %v", err)
			}
			return nil
		}
		if err != nil {
			log.Fatalf("srv.Recv failed: %v", err)
		}
		feedNames = append(feedNames, feedElem.Name)
	}
}

// 添加单个 Feed （一元RPC）
func (rb *rssBackend) AddFeed(ctx context.Context, req *feed.Feed) (*feed.Status, error) {
	i, err := r.SAdd("feeds", req.Name).Result()
	return &feed.Status{Count: i}, err
}

// 添加多个 Feed （一元RPC）
func (rb *rssBackend) AddFeedGroup(ctx context.Context, req *feed.FeedGroup) (*feed.Status, error) {
	i, err := r.SAdd("feeds", req.FeedNames).Result()
	return &feed.Status{Count: i}, err
}

// 订阅并立即刷新 (双向流RPC)
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
				r.SAdd("feeds", feedElem.Name)
				UpdateDB([]string{feedElem.Name})
			}

			items, err := r.LRange(feedElem.Name, 0, -1).Result()
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
					PubTime:     rec.PublishedParsed.String(),
				}
				if err = srv.Send(story); err != nil {
					log.Fatalf("srv.Send failed: %v", err)
				}
			}
		}()
	}
}

// 前端服务器获取 Feed （服务端流式RPC）
func (rb *rssBackend) GetFeed(req *feed.Feed, srv feed.RSS_GetFeedServer) error {
	items, err := r.LRange(req.Name, req.Start, req.End).Result()
	if err != nil {
		log.Fatalf("r.LRange failed: %v", err)
	}

	startTime, err := time.Parse(layout, req.StartTime)
	if err != nil {
		log.Fatalf("time.Parse failed: %v", err)
	}
	endTime, err := time.Parse(layout, req.EndTime)
	if err != nil {
		log.Fatalf("time.Parse failed: %v", err)
	}

	for _, item := range items {
		rec := &gofeed.Item{}
		if err := json.Unmarshal([]byte(item), rec); err != nil {
			log.Fatalf("json.Unmarshal failed: %v", err)
		}
		if rec.PublishedParsed.After(endTime) {
			continue
		}
		if rec.PublishedParsed.Before(startTime) {
			break
		}

		for _, keyword := range strings.Split(req.Keywords, " ") {
			if strings.Contains(rec.Title, keyword) {
				story := feed.Story{
					Title:       rec.Title,
					Link:        rec.Link,
					Description: rec.Description,
					PubTime:     rec.PublishedParsed.String(),
				}
				if err = srv.Send(&story); err != nil {
					log.Fatalf("srv.Send failed: %v", err)
				}
			}
		}
	}
	return nil
}

// 获取多个Feed （服务端流式RPC）
func (rb *rssBackend) GetFeedGroup(req *feed.FeedGroup, srv feed.RSS_GetFeedGroupServer) error {
	if req.Nums == 0 {
		req.Nums = 1<<63 - 1
	}
	if req.StartTime == "" {
		req.StartTime = "2006-01-02T15:04"
	}
	if req.EndTime == "" {
		req.EndTime = "2026-01-02T15:04"
	}
	var wg sync.WaitGroup
	var err error
	for _, name := range req.FeedNames {
		feed := feed.Feed{
			Name:      name,
			Start:     0,
			End:       req.Nums/int64(len(req.FeedNames)) - 1,
			StartTime: req.StartTime,
			EndTime:   req.EndTime,
			Keywords:  req.Keywords,
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err = rb.GetFeed(&feed, srv); err != nil {
				log.Fatalf("rb.GetFeed failed: %v", err)
			}
		}()
	}
	wg.Wait()
	return nil
}

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

				// 判断之前先从新到旧的顺序排序feed.Items
				sort.Slice(feed.Items, func(i, j int) bool {
					return feed.Items[i].PublishedParsed.After(*feed.Items[j].PublishedParsed)
				})
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
	lis, err := net.Listen("tcp", "localhost:8888")
	if err != nil {
		log.Fatalf("net.Listen failed: %v\n", err)
	}
	log.Println("Listening and serving HTTP on localhost:8888")

	grpcServer := grpc.NewServer()
	feed.RegisterRSSServer(grpcServer, &rssBackend{})
	grpcServer.Serve(lis)
}
