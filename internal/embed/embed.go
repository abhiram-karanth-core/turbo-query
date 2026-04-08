package embed

import (
	"math"
	"sync"
	"time"

	"github.com/knights-analytics/hugot"
	"github.com/knights-analytics/hugot/options"
	"github.com/knights-analytics/hugot/pipelines"
)

const Dim = 384

var (
	pipeline *pipelines.FeatureExtractionPipeline
	initOnce sync.Once
)

type embedRequest struct {
	query  string
	result chan embedResult
}

type embedResult struct {
	vec []float32
	err error
}

var embedQueue = make(chan embedRequest, 256)

func Init() error {
	var err error
	initOnce.Do(func() {
		session, e := hugot.NewORTSession(
			options.WithIntraOpNumThreads(1),
			options.WithInterOpNumThreads(1),
			options.WithCPUMemArena(false),
			options.WithMemPattern(false),
		)
		if e != nil {
			err = e
			return
		}
		pipeline, err = hugot.NewPipeline(session, hugot.FeatureExtractionConfig{
			ModelPath: "./models/all-MiniLM-L6-v2",
			Name:      "all-MiniLM-L6-v2",
		})
		if err != nil {
			return
		}
		go batchEmbedWorker()
	})
	return err
}

func batchEmbedWorker() {
	for {
		first := <-embedQueue
		batch := []embedRequest{first}
		timer := time.NewTimer(5 * time.Millisecond)
	drain:
		for len(batch) < 32 {
			select {
			case req := <-embedQueue:
				batch = append(batch, req)
            case <-timer.C:
                break drain
			default:
				break drain
			}
		} 
        timer.Stop()
		queries := make([]string, len(batch))
		for i, r := range batch {
			queries[i] = r.query
		}
		results, err := pipeline.RunPipeline(queries)
		for i, r := range batch {
			if err != nil {
				r.result <- embedResult{err: err}
			} else {
				r.result <- embedResult{vec: normalize(results.Embeddings[i])}
			}
		}
	}
}

func GetEmbedding(query string) ([]float32, error) {
	ch := make(chan embedResult, 1)
	embedQueue <- embedRequest{query: query, result: ch}
	res := <-ch
	return res.vec, res.err
}

func normalize(v []float32) []float32 {
	var sum float32
	for _, x := range v {
		sum += x * x
	}
	norm := float32(1.0 / math.Sqrt(float64(sum)))
	out := make([]float32, len(v))
	for i, x := range v {
		out[i] = x * norm
	}
	return out
}
