# Turbo Query — Distributed Hybrid Search Engine

Turbo Query is a distributed search system combining BM25 keyword retrieval with semantic vector search using MiniLM embeddings, built to demonstrate production-style search infrastructure.

---

## Features

- **Hybrid search:** BM25 + semantic reranking via MiniLM embeddings
- **ONNX Runtime inference** — MiniLM embeddings via hugot, ~2–4ms per query
- **Memory-mapped vector store** for zero-copy reads
- **Dual caching** — Redis query cache + Linux page cache for mmap vector pages
- **Singleflight deduplication** — concurrent identical cache misses collapse into one pipeline execution
- **Consistent hash–based sharding** across 4 shard nodes
- **Parallel fan-out** coordinator with per-shard HTTP servers (Go + chi)
- **Fully containerized** with Docker Compose
- **300,000+ documents** indexed across 4 shards (~75k docs/shard)

---

## Architecture

```
User Query
    ↓
Redis Query Cache
    ↓ (cache miss)
Singleflight dedup
    ↓
Coordinator (parallel fan-out)
    ↓
Shard 0  Shard 1  Shard 2  Shard 3
    ↓
BM25 retrieve → Vector rerank → Hybrid score
    ↓
Top-K merge → Cache result in Redis → Response
```

Each shard maintains:
- A **Bleve** inverted index for BM25 keyword retrieval
- A **memory-mapped dense vector store** (`vectors.bin`) for zero-copy vector reads
- **Shard-local sequential document IDs** as direct offsets into the vector file

### Caching

Turbo Query uses two complementary caching layers:

**Redis query cache** — caches full serialized search responses at the coordinator. Cache hits bypass the entire pipeline (embed, fan-out, rerank) and return in ~1–2ms. Cache misses fall through to the full distributed search pipeline.

**Linux page cache** — mmap-backed vector reads (`vectors.bin`) are automatically cached by the OS after first access. Subsequent vector lookups on cache misses hit RAM, not disk, eliminating I/O latency from the hot path.

### Singleflight

Concurrent requests for the same uncached query collapse into a single pipeline execution via `golang.org/x/sync/singleflight`. All waiting goroutines receive the same result, preventing cache stampede without Redis locking.

---

## Tech Stack

| Layer | Technology |
|---|---|
| Language | Go |
| HTTP Router | chi |
| Inverted Index / BM25 | Bleve |
| Embeddings | all-MiniLM-L6-v2 via ONNX Runtime (hugot) |
| Query Cache | Redis (allkeys-lru eviction) |
| Deduplication | golang.org/x/sync/singleflight |
| Containerization | Docker Compose |
| Vector Storage | mmap-backed binary file |

---

## Project Structure

```
cmd/
  api/            # coordinator server (fan-out + cache layer)
  shard/          # per-shard search server

internal/
  embed/          # ONNX Runtime embedding client + L2 normalization
  shardnode/      # shard HTTP handlers and hybrid search logic
  data/           # shard indexes and vector files

indexer/          # offline indexing pipeline
```

---

## Indexing Pipeline

1. Wiki JSON ingestion
2. MiniLM embedding generation
3. L2 vector normalization and mmap write
4. Consistent hash routing to shards
5. Sequential shard-local ID assignment
6. Bleve batch indexing with shard-local ID as document ID

> Vectors are L2-normalized at index time so cosine similarity reduces to a dot product at query time.

---

## Scoring

Each shard performs:

1. BM25 candidate retrieval (top 100)
2. Cosine similarity via dot product against mmap-backed vectors
3. Score normalization
4. Hybrid fusion:

```
final_score = 0.7 × BM25_norm + 0.3 × cosine_norm
```

> Reranking runs only over BM25 top-100 candidates — cost is constant regardless of shard size.

---

## Performance

Benchmarked on a laptop (WSL2, Intel i7-13650HX, 16GB RAM, WSL2 limited to 10GB) using wrk2, which corrects for coordinated omission.

### Full Pipeline — No Cache

All queries salted to guarantee zero cache hits. Every request runs the complete pipeline: embed → fan-out → BM25 → rerank → merge.

```bash
docker exec turbo-query-redis-1 redis-cli FLUSHALL
wrk2 -t4 -c20 -d60s -R 80 --latency -s search.lua http://localhost:8080/search
```

| Percentile | Latency |
|---|---|
| P50 | 66ms |
| P75 | 77ms |
| P90 | 89ms |
| P95 | 102ms |
| P99 | 125ms |
| P99.9 | 198ms |
| Max | 211ms |

**80 RPS, 20 concurrent connections, 60s duration, 4800 requests, 0 cache hits (deliberately — pure raw performance).**

Latency scales with concurrency due to the hugot single-session constraint — at 50 concurrent 
connections (same 80 RPS), P90 doubles to 212ms as goroutines queue behind the ONNX inference call.

### Latency Breakdown (single request, no contention)

| Component | Latency |
|---|---|
| Redis cache hit | ~1–2ms |
| ONNX embedding (MiniLM) | ~2–4ms |
| BM25 + parallel fan-out + rerank (4 shards) | ~5–10ms |
| **Full pipeline (cache miss, sequential)** | **~15–45ms** |

---

## Known Limitations

The hugot ONNX Runtime backend enforces a single active session globally, serializing all concurrent embedding calls internally. This limits full-pipeline throughput under high concurrency.

A session pool was attempted using `yalue/onnxruntime_go` which supports multiple independent sessions. However, the required pure Go BERT tokenizer added ~14–20ms per call vs hugot's ~2–4ms, making it worse overall. The correct fix would pair `yalue/onnxruntime_go` with `daulet/tokenizers` rust bindings for fast tokenization alongside concurrent sessions.

The shard pipeline — BM25, mmap vector reads, parallel fan-out, hybrid rerank — is not the bottleneck. All 4 shards respond within ~5–10ms under load.

The Linux page cache warms at the BM25 retrieval level, not the semantic level. Lexically similar queries like "microsoft" → "microsoft stocks" return overlapping docIDs from BM25, so their vector pages are already warm in the page cache — effectively free RAM reads. Semantically similar but lexically different queries like "microsoft" → "bill gates company" return entirely different docIDs from BM25, causing cold page faults despite being conceptually identical. Page cache locality follows lexical similarity, not semantic similarity — an inherent tradeoff of BM25-first hybrid retrieval.

---

## Querying the API

```bash
curl -s -X POST http://localhost:8080/search \
  -H "Content-Type: application/json" \
  -d '{"query":"byzantine empire fall","top_k":5}' \
  | python -m json.tool
```

### Example Response

```json
[
  {
    "doc_id": "14823",
    "score": 0.9341,
    "shard_id": "2",
    "title": "Fall of Constantinople",
    "text": "The fall of Constantinople in 1453 marked the end of the Byzantine Empire..."
  },
  {
    "doc_id": "9217",
    "score": 0.8976,
    "shard_id": "0",
    "title": "Byzantine Empire",
    "text": "The Byzantine Empire, also known as the Eastern Roman Empire..."
  }
]
```