package embed

import (
	"bytes"
	"encoding/json"
	"io"
	"math"
	"net/http"
	"time"
)

const Dim = 384

var httpClient = &http.Client{
	Timeout: 60 * time.Second,
}

const ollamaURL = "http://localhost:11434/api/embeddings"
const modelName = "all-minilm"

type embeddingRequest struct {
	Model  string `json:"model"`
	Prompt string `json:"prompt"`
}

type embeddingResponse struct {
	Embedding []float64 `json:"embedding"`
}

func normalize(vec []float32) {
	var sum float64
	for _, v := range vec {
		sum += float64(v * v)
	}

	norm := float32(math.Sqrt(sum))
	if norm == 0 {
		return
	}

	inv := 1.0 / norm
	for i := range vec {
		vec[i] *= inv
	}
}
func Embed(text string) []float32 {
	reqBody := embeddingRequest{
		Model:  modelName,
		Prompt: text,
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		panic(err)
	}

	req, err := http.NewRequest("POST", ollamaURL, bytes.NewBuffer(jsonData))
	if err != nil {
		panic(err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		panic(err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		panic(err)
	}

	var result embeddingResponse
	if err := json.Unmarshal(body, &result); err != nil {
		panic(err)
	}

	vec := make([]float32, len(result.Embedding))
	for i, v := range result.Embedding {
		vec[i] = float32(v)
	}
	normalize(vec)
	return vec
}
//pre normalizing the vectors so that Cosine becomes dot product only.
//global normalization
//more consistent ranking