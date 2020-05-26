package main

import (
	"context"
	"fmt"
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
	conn, err := grpc.Dial("0.0.0.0:8888", grpc.WithInsecure())
	if err != nil {
		log.Fatalf("grpc.Dial failed: %v", err)
	}
	defer conn.Close()

	client = feed.NewRSSClient(conn)

	// ctx, cancel := context.WithTimeout(context.Background(), 500*time.Second)
	// defer cancel()

	router := gin.Default()
	// router.LoadHTMLFiles("../webpages/rss.html")
	router.Static("/", "/home/jack/Documents/git/myGoFeed/webpages")

	// router.POST("/world", func(ginCtx *gin.Context) {
	// 	ok := feed.OK{OK: true}
	// 	RSSClient, err := client.GetAllFeedNames(context.Background(), &ok)
	// 	if err != nil {
	// 		log.Fatalf("client.GetAllFeedNames failed: %v", err)
	// 	}
	// 	feedNames := make([]string, 0)
	// 	for {
	// 		feedElem, err := RSSClient.Recv()
	// 		if err == io.EOF {
	// 			ginCtx.JSON(http.StatusOK, feedNames)
	// 			return
	// 		}
	// 		if err != nil {
	// 			log.Fatalf("RSSClient.Recv failed: %v", err)
	// 		}
	// 		feedNames = append(feedNames, feedElem.Name)
	// 	}
	// })

	router.POST("/allFeedNamesAndStories", allFeedNamesAndStoriesHandler)
	router.POST("/addAndRefresh", addAndRefreshHandler)
	router.POST("/deleteChecked", deleteCheckedHandler)
	router.POST("/hello", helloHandler)
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
	}

	getFeedGroupClient, err := client.GetFeedGroup(context.Background(), &feedGroup)
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

func helloHandler(ginCtx *gin.Context) {
	feedElem := feed.Feed{
		Name:      ginCtx.PostForm("FeedName"),
		Start:     0,
		End:       -1,
		StartTime: "2020-05-24 00:00:00",
		EndTime:   "2020-05-25 00:00:00",
	}
	RSSClient, err := client.GetFeed(context.Background(), &feedElem)
	if err != nil {
		log.Fatalf("client.GetFeed failed: %v", err)
	}

	stories := make([]*feed.Story, 0)
	for {
		story, err := RSSClient.Recv()
		if err == io.EOF {
			ginCtx.JSON(http.StatusOK, stories)
			return
		}
		if err != nil {
			log.Fatalf("RSSClient.Recv failed: %v", err)
		}
		stories = append(stories, story)
	}
}

func allFeedNamesAndStoriesHandler(ginCtx *gin.Context) {
	ok := feed.OK{OK: true}
	getAllFeedNamesClient, err := client.GetAllFeedNames(context.Background(), &ok)
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
	getFeedGroupClient, err := client.GetFeedGroup(context.Background(), &feedGroup)
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
	deleteFeedsClient, err := client.DeleteFeeds(context.Background())
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
	subAndRefClient, err := client.SubscribeAndRefresh(context.Background())
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

func toBackend() {

	conn, err := grpc.Dial("localhost:8888", grpc.WithInsecure())
	if err != nil {
		log.Fatalf("grpc.Dial failed: %v", err)
	}
	defer conn.Close()

	client := feed.NewRSSClient(conn)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Second)
	defer cancel()

	clientSAR, err := client.SubscribeAndRefresh(ctx)
	if err != nil {
		log.Fatalf("client.SubscribeAndRefresh failed: %v", err)
	}

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		for {
			story, err := clientSAR.Recv()
			if err == io.EOF {
				wg.Done()
				return
			}
			if err != nil {
				log.Fatalf("clientSAR.Recv failed: %v", err)
			}

			fmt.Println(story.Title)
		}
	}()

	for _, url := range []string{
		"https://rss.nytimes.com/services/xml/rss/nyt/HomePage.xml",
		"https://feeds.bbci.co.uk/news/rss.xml",
	} {
		feedElem := &feed.Feed{
			Name: url,
		}
		if err = clientSAR.Send(feedElem); err != nil {
			log.Fatalf("stream.Send failed: %v", err)
		}
	}

	if err = clientSAR.CloseSend(); err != nil {
		log.Fatalf("clientSAR.CloseSend failed: %v", err)
	}
	wg.Wait()
	// time.Sleep(1 * time.Hour)
	// feedElem := feed.Feed{
	// 	Name:      "https://rss.nytimes.com/services/xml/rss/nyt/HomePage.xml",
	// 	Start:     0,
	// 	End:       -1,
	// 	StartTime: "2020-05-23 12:00:00",
	// 	EndTime:   "2020-05-24 06:46:00",
	// // }
	// fg := feed.FeedGroup{
	// 	FeedNames: []string{
	// 		"https://rss.nytimes.com/services/xml/rss/nyt/HomePage.xml",
	// 		"https://feeds.bbci.co.uk/news/rss.xml",
	// 	},
	// 	Nums:      1<<63 - 1,
	// 	StartTime: "2020-05-23 12:00:00",
	// 	EndTime:   "2020-05-24 06:46:00",
	// }
	// stream, err := client.GetFeed(ctx, &feedElem)
	// stream, err := client.GetFeedGroup(ctx, &fg)
	// status, err := client.AddFeed(ctx, &feed.Feed{Name: "https://rss.nytimes.com/services/xml/rss/nyt/HomePage.xml"})
	// if err != nil {
	// 	log.Fatalf("client.AddFeed failed: %v", err)
	// }

	// status, err := client.AddFeedGroup(ctx, &feed.FeedGroup{
	// 	FeedNames: []string{"https://rss.nytimes.com/services/xml/rss/nyt/HomePage.xml", "https://feeds.bbci.co.uk/news/rss.xml"},
	// })

	// for {
	// 	story, err := stream.Recv()
	// 	if err == io.EOF {
	// 		break
	// 	}
	// 	if err != nil {
	// 		log.Fatalf("stream.Recv failed: %v", err)
	// 	}

	// 	fmt.Println(story.Link)
	// }

}
