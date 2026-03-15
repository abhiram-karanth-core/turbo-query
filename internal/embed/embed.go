package embed

import (
	"math"
	"sync"

	"github.com/knights-analytics/hugot"
	"github.com/knights-analytics/hugot/options"
	"github.com/knights-analytics/hugot/pipelines"
)

const Dim = 384

var (
	pipeline *pipelines.FeatureExtractionPipeline
	mu       sync.Mutex
	initOnce sync.Once
)
func Init() error {
    var err error
    initOnce.Do(func() {
        session, e := hugot.NewORTSession(
            options.WithIntraOpNumThreads(8),
            options.WithInterOpNumThreads(4),
            options.WithExecutionMode(true),
        )
        if e != nil { err = e; return }
        pipeline, err = hugot.NewPipeline(session, hugot.FeatureExtractionConfig{
            ModelPath: "./models/all-MiniLM-L6-v2",
            Name:      "all-MiniLM-L6-v2",
        })
    })
    return err
}

func GetEmbedding(query string) ([]float32, error) {
    result, err := pipeline.RunPipeline([]string{query})
    if err != nil { return nil, err }
    return normalize(result.Embeddings[0]), nil
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