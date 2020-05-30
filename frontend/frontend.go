package main

import (
	"context"
	"io"
	"log"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/369329303/myGoFeed/feed"
	"github.com/gin-gonic/gin"
	"google.golang.org/grpc"
)

// FeedNamesAndStories 所有Feed 名称和故事
type FeedNamesAndStories struct {
	FeedNames []string
	Stories   []*feed.Story
}

var client feed.RSSClient

func main() {
	conn, err := grpc.Dial("localhost:8888", grpc.WithInsecure())
	if err != nil {
		log.Fatalf("grpc.Dial failed: %v", err)
	}
	defer conn.Close()

	client = feed.NewRSSClient(conn)

	router := gin.Default()
	router.Static("/", "/home/jack/Documents/git/myGoFeed/frontend/webpages")

	router.POST("/allFeedNamesAndStories", allFeedNamesAndStoriesHandler)
	router.POST("/addAndRefresh", addAndRefreshHandler)
	router.POST("/deleteChecked", deleteCheckedHandler)
	router.POST("/filterFeeds", filterFeedsHandler)
	router.Run()
}

func filterFeedsHandler(ginCtx *gin.Context) {
	start, err := strconv.Atoi(ginCtx.PostForm("start"))
	if err != nil {
		log.Fatalf("ginCtx.PostForm failed: %v", err)
	}
	end, err := strconv.Atoi(ginCtx.PostForm("end"))
	if err != nil {
		log.Fatalf("ginCtx.PostForm failed: %v", err)
	}
	feedGroup := feed.FeedGroup{
		FeedNames: ginCtx.PostFormArray("feedName"),
		Nums:      int64(end - start + 1),
		StartTime: ginCtx.PostForm("startTime"),
		EndTime:   ginCtx.PostForm("endTime"),
		Keywords:  ginCtx.PostForm("keywords"),
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	getFeedGroupClient, err := client.GetFeedGroup(ctx, &feedGroup)
	if err != nil {
		log.Fatalf("failed: %v", err)
	}

	stories := make([]*feed.Story, 0)
	for {
		story, err := getFeedGroupClient.Recv()
		if err == io.EOF {
			ginCtx.JSON(http.StatusOK, stories)
			return
		}
		if err != nil {
			log.Fatalf("getFeedGroupClient.Recv failed: %v", err)
		}
		stories = append(stories, story)
	}
}

func allFeedNamesAndStoriesHandler(ginCtx *gin.Context) {
	ok := feed.OK{OK: true}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	getAllFeedNamesClient, err := client.GetAllFeedNames(ctx, &ok)
	if err != nil {
		log.Fatalf("client.GetAllFeedNames failed: %v", err)
	}
	feedNames := make([]string, 0)
	for {
		feedElem, err := getAllFeedNamesClient.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			log.Fatalf("RSSClient.Recv failed: %v", err)
		}
		feedNames = append(feedNames, feedElem.Name)
	}
	feedGroup := feed.FeedGroup{FeedNames: feedNames, Nums: 10}
	ctx2, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	getFeedGroupClient, err := client.GetFeedGroup(ctx2, &feedGroup)
	if err != nil {
		log.Printf("client.GetFeedGroup failed: %v", err)
	}
	stories := make([]*feed.Story, 0)
	for {
		story, err := getFeedGroupClient.Recv()
		if err == io.EOF {
			sort.Slice(stories, func(i, j int) bool {
				if stories[i].PubTime > stories[j].PubTime {
					return true
				}
				return false
			})
			fNAS := FeedNamesAndStories{
				FeedNames: feedNames,
				Stories:   stories,
			}
			ginCtx.JSON(http.StatusOK, fNAS)
			return
		}
		if err != nil {
			log.Fatalf("getFeedGroupClient.Recv failed: %v", err)
		}
		stories = append(stories, story)
	}
}

func deleteCheckedHandler(ginCtx *gin.Context) {
	feedNames := ginCtx.PostFormArray("feedName")
	log.Println(feedNames)
	log.Println(ginCtx.PostForm("start"))
	if len(feedNames) == 0 {
		ginCtx.String(http.StatusBadRequest, "Empty parameters")
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	deleteFeedsClient, err := client.DeleteFeeds(ctx)
	if err != nil {
		log.Fatalf("client.DeleteFeeds failed: %v", err)
	}

	for _, feedName := range feedNames {
		feedElem := feed.Feed{Name: feedName}
		if err = deleteFeedsClient.Send(&feedElem); err != nil {
			log.Fatalf("deleteFeedsClient.Send failed: %v", err)
		}
	}
	status, err := deleteFeedsClient.CloseAndRecv()
	if err != nil {
		log.Fatalf("deleteFeedsClient.CloseAndRecv failed: %v", err)
	}
	ginCtx.JSON(http.StatusOK, status)
}

func addAndRefreshHandler(ginCtx *gin.Context) {
	if ginCtx.PostForm("feedNames") == "" {
		ginCtx.String(http.StatusBadRequest, "Empty parameters")
		return
	}

	feedNames := strings.Split(ginCtx.PostForm("feedNames"), "\n")

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	subAndRefClient, err := client.SubscribeAndRefresh(ctx)
	if err != nil {
		log.Fatalf("client.SubscribeAndRefresh failed: %v", err)
	}

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		stories := make([]*feed.Story, 0)
		for {
			story, err := subAndRefClient.Recv()
			if err == io.EOF {
				ginCtx.JSON(http.StatusOK, stories)
				return
			}
			if err != nil {
				log.Printf("subAndRefClient.Recv failed %v", err)
			}
			stories = append(stories, story)
		}
	}()

	for _, feedName := range feedNames {
		feedElem := feed.Feed{
			Name: feedName,
		}
		if err = subAndRefClient.Send(&feedElem); err != nil {
			log.Fatalf("subAndRefClient.Send failed: %v", err)
		}
	}
	if err = subAndRefClient.CloseSend(); err != nil {
		log.Fatalf("subAndRefClient.CloseSend failed: %v", err)
	}
	wg.Wait()
}
