package server

import (
	"bytes"
	"encoding/json"
	"log"
	"net/http"
	"sort"
	"sync"
	"time"
	"turbo-query/internal/embed"
)

func (s *Server) SearchHandler(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	defer func() {
		log.Printf("fanout latency=%v",
			time.Since(start),
		)
	}()
	var req struct {
		Query  string    `json:"query"`
		TopK   int       `json:"top_k"`
		Vector []float32 `json:"vector"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	if req.TopK <= 0 {
		req.TopK = 10
	}
	ctx := r.Context()
	cacheKey := "search:" + req.Query

	if cached, err := s.redisClient.Get(ctx, cacheKey); err == nil {
		log.Printf("cache HIT query=%q", req.Query)
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Cache", "HIT")
		w.Write(cached)
		return
	}
	val, err, _ := s.sf.Do(cacheKey, func() (interface{}, error) {

		results, err := s.FanoutSearch(req.Query)
		if err != nil {
			return nil, err
		}

		encoded, err := json.Marshal(results)
		if err != nil {
			return nil, err
		}

		s.redisClient.Set(ctx, cacheKey, encoded, 5*time.Minute)

		return encoded, nil
	})
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	encoded := val.([]byte)

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Cache", "MISS")
	w.Write(encoded)
}
func (s *Server) FanoutSearch(query string) ([]Result, error) {

	var wg sync.WaitGroup
	resultsChan := make(chan []Result, len(s.shards))
	embedStart := time.Now()
	qvec := embed.Embed(query)
	log.Printf("embed latency=%v", time.Since(embedStart))
	for _, shard := range s.shards {

		wg.Add(1)

		go func(shardURL string) {
			defer wg.Done()

			res, err := s.queryShard(shardURL, query, qvec)
			if err != nil {
				log.Println("shard error:", shardURL, err)
				return
			}

			log.Println("shard responded:", shardURL, "hits:", len(res))

			resultsChan <- res

		}(shard)
	}

	wg.Wait()
	close(resultsChan)

	var allResults []Result

	for r := range resultsChan {
		allResults = append(allResults, r...)
	}

	return mergeTopK(allResults, 10), nil
}

func (s *Server) queryShard(shardURL, query string, qvec []float32) ([]Result, error) {

	body := map[string]interface{}{
		"query":  query,
		"top_k":  10,
		"vector": qvec,
	}

	buf, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest(
		"POST",
		shardURL+"/search",
		bytes.NewBuffer(buf),
	)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var shardResp struct {
		Hits []Result `json:"hits"`
	}

	err = json.NewDecoder(resp.Body).Decode(&shardResp)
	if err != nil {
		return nil, err
	}

	return shardResp.Hits, nil
}
func mergeTopK(results []Result, k int) []Result {

	sort.Slice(results, func(i, j int) bool {
		return results[i].Score > results[j].Score
	})

	if len(results) > k {
		return results[:k]
	}

	return results
}
