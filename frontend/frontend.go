package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"sync"
	"time"

	"github.com/369329303/myGoFeed/feed"
	"google.golang.org/grpc"
)

func main() {
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
	time.Sleep(1 * time.Hour)
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
