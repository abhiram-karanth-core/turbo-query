package shardnode

import (
	"encoding/binary"
	"encoding/json"
	"log"
	"math"
	"net/http"
	"sort"
	"strconv"
	"time"
	"turbo-query/internal/embed"

	"github.com/blevesearch/bleve/v2"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
)

const (
	rerankWindow = 100 // BM25 candidates
	vectorDim    = 384
)

func (s *Server) getVector(docID uint32) []float32 {
	start := int(docID) * vectorDim * 4
	end := start + vectorDim*4

	if start < 0 || end > len(s.mmapBuf) {
		log.Printf(
			"vector OOB: docID=%d start=%d end=%d mmap=%d",
			docID, start, end, len(s.mmapBuf),
		)
		return nil
	}

	raw := s.mmapBuf[start:end]

	vec := make([]float32, vectorDim)
	for i := 0; i < vectorDim; i++ {
		bits := binary.LittleEndian.Uint32(raw[i*4:])
		vec[i] = math.Float32frombits(bits)
	}
	return vec
}
func dot(a, b []float32) float64 {
	var sum float64
	for i := 0; i < len(a); i++ {
		sum += float64(a[i] * b[i])
	}
	return sum
}
func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	defer func() {
		log.Printf("shard=%s latency=%v",
			s.shardID,
			time.Since(start),
		)
	}()
	var req SearchRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	log.Println("received search:", req.Query)
	if req.TopK <= 0 {
		req.TopK = 10
	}

	qvec := embed.Embed(req.Query)
	if len(qvec) == 0 {
		http.Error(w, "embedding failed", http.StatusInternalServerError)
		return
	}

	query := bleve.NewMatchQuery(req.Query)

	searchReq := bleve.NewSearchRequestOptions(query, rerankWindow, 0, false)
	searchReq.Fields = []string{"title", "text"}
	res, err := s.index.Search(searchReq)
	if err != nil {
		http.Error(w, "search failed", http.StatusInternalServerError)
		return
	}

	if len(res.Hits) == 0 {
		json.NewEncoder(w).Encode(SearchResponse{})
		return
	}

	maxBM25 := res.Hits[0].Score
	if maxBM25 == 0 {
		maxBM25 = 1
	}

	hits := make([]SearchHit, 0, req.TopK)

	for _, hit := range res.Hits {
		docID64, _ := strconv.ParseUint(hit.ID, 10, 32)
		docID := uint32(docID64)

		dvec := s.getVector(docID)
		if len(dvec) == 0 {
			continue
		}

		cos := dot(qvec, dvec)
		normCos := (cos + 1) / 2

		normBM25 := hit.Score / maxBM25

		final := 0.7*normBM25 + 0.3*normCos

		var title, text string

		if v, ok := hit.Fields["title"].(string); ok {
			title = v
		}
		if v, ok := hit.Fields["text"].(string); ok {
			text = v
		}
		hits = append(hits, SearchHit{
			DocID:   hit.ID,
			Score:   final,
			ShardID: s.shardID,
			Title:   title,
			Text:    text,
		})
	}

	sort.Slice(hits, func(i, j int) bool {
		return hits[i].Score > hits[j].Score
	})

	if len(hits) > req.TopK {
		hits = hits[:req.TopK]
	}

	json.NewEncoder(w).Encode(SearchResponse{Hits: hits})
}
func (s *Server) RegisterRoutes() http.Handler {
	r := chi.NewRouter()

	r.Use(middleware.Logger)

	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   []string{"*"},
		AllowedMethods:   []string{"GET", "POST"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type"},
		AllowCredentials: false,
		MaxAge:           300,
	}))

	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	r.Post("/search", s.handleSearch)
	return r
}
