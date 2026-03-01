package server

import (
	"fmt"
	"net/http"
	"os"
	"strconv"
	"time"

	_ "github.com/joho/godotenv/autoload"
)

type Server struct {
	port       int
	httpClient *http.Client
	shards     []string
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

	srv := &Server{
		port: port,
		httpClient: &http.Client{
			Timeout: 2 * time.Second,
		},
		shards: []string{
			"http://shard0:8080",
			"http://shard1:8080",
			"http://shard2:8080",
			"http://shard3:8080",
		},
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
