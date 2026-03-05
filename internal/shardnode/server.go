package shardnode

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/blevesearch/bleve/v2"
	"github.com/blevesearch/mmap-go"
	_ "github.com/joho/godotenv/autoload"
)

type Server struct {
	port    int
	shardID string
	index   bleve.Index
	mmapBuf mmap.MMap
}

func (s *Server) Close() {
	if s.mmapBuf != nil {
		s.mmapBuf.Unmap()
	}
}

func NewServer() *http.Server {
	portStr := os.Getenv("PORT")
	if portStr == "" {
		portStr = "8080"
	}

	port, _ := strconv.Atoi(portStr)

	shardID := os.Getenv("SHARD_ID")
	if shardID == "" {
		shardID = "0"
	}

	log.Println("starting shard:", shardID)

	indexPath := "/data/index.bleve"
	vectorPath := "/data/vectors.bin"

	idx, err := bleve.Open(indexPath)
	if err != nil {
		log.Fatalf("failed to open index: %v", err)
	}

	vecFile, err := os.Open(vectorPath)
	if err != nil {
		log.Fatalf("failed to open vectors: %v", err)
	}

	mmapBuf, err := mmap.Map(vecFile, mmap.RDONLY, 0)
	if err != nil {
		log.Fatalf("mmap failed: %v", err)
	}

	s := &Server{
		port:    port,
		shardID: shardID,
		index:   idx,

		mmapBuf: mmapBuf,
	}

	return &http.Server{
		Addr:         fmt.Sprintf(":%d", port),
		Handler:      s.RegisterRoutes(),
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 30 * time.Second,
	}
}
