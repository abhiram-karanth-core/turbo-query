package server

import (
	"fmt"
	"net/http"
	"os"
	"strconv"
	"time"

	"golang.org/x/sync/singleflight"

	redisclient "turbo-query/internal/redis"

	_ "github.com/joho/godotenv/autoload"
)

type Server struct {
	port        int
	httpClient  *http.Client
	shards      []string
	redisClient *redisclient.Client
	sf          singleflight.Group
}
type Result struct {
	DocID   string  `json:"doc_id"`
	Score   float64 `json:"score"`
	ShardID string  `json:"shard_id"`
	Title   string  `json:"title"`
	Text    string  `json:"text"`
}

func NewServer() *http.Server {
	portStr := os.Getenv("PORT")
	if portStr == "" {
		portStr = "8080"
	}

	port, err := strconv.Atoi(portStr)
	if err != nil {
		port = 8080
	}
	redisAddr := os.Getenv("REDIS_ADDR")
	if redisAddr == "" {
		redisAddr = "redis:6379"
	}
	srv := &Server{
		port: port,
		httpClient: &http.Client{
			Timeout: 2 * time.Second,
			Transport: &http.Transport{
				MaxIdleConns:        600,
				MaxIdleConnsPerHost: 150,
				IdleConnTimeout:     90 * time.Second,
				DisableKeepAlives:   false,
			},
		},
		shards: []string{
			"http://shard0:8080",
			"http://shard1:8080",
			"http://shard2:8080",
			"http://shard3:8080",
		},
		redisClient: redisclient.NewClient(redisAddr),
	}

	server := &http.Server{
		Addr:         fmt.Sprintf(":%d", srv.port),
		Handler:      srv.RegisterRoutes(),
		IdleTimeout:  time.Minute,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 30 * time.Second,
	}

	return server
}
