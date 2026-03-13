# Turbo Query — Distributed Hybrid Search Engine

Turbo Query is a high-performance distributed search system that combines BM25 keyword retrieval with semantic vector search using MiniLM embeddings. The project demonstrates production-style search infrastructure concepts including sharding, fan-out querying, mmap vector storage, Redis query-result caching, and hybrid ranking.

---

## Features

- **Hybrid search:** BM25 + semantic reranking
- **MiniLM embeddings** via Ollama
- **Memory-mapped vector store** for zero-copy reads
- **Redis query-result caching** to reduce repeated search latency
- **Consistent hash–based sharding**
- **Per-shard HTTP servers** (Go + chi)
- **Fully containerized** with Docker Compose
- **Per-shard latency instrumentation**
- **Robust handling** of embedding and out-of-bounds edge cases
- **300,000+ documents** indexed across 4 shards (~75k docs/shard)

---

## Architecture

```
User Query
    ↓
Redis Query Cache
    ↓ (cache miss)
Coordinator (fan-out)
    ↓
Shard Servers (parallel)
    ↓
BM25 retrieve → Vector rerank → Hybrid score
    ↓
Top-K merge → Cache result in Redis → Response
```

Each shard maintains:

- A **Bleve** inverted index for keyword retrieval
- A **memory-mapped dense vector store** (`vectors.bin`)
- **Shard-local sequential document IDs** used for both Bleve and vector offsets
- An HTTP search endpoint

### Query Cache Layer

Turbo Query includes an optional Redis-based query cache at the coordinator layer. Repeated queries can be served directly from Redis without executing shard fan-out.

- **Cache hits** bypass the search pipeline entirely, reducing latency to sub-millisecond levels.
- **Cache misses** fall back to the full distributed search pipeline.

---

## Tech Stack

| Layer | Technology |
|---|---|
| Language | Go |
| HTTP Router | chi |
| Inverted Index/BM25 | Bleve |
| Embeddings | all-MiniLM-L6-v2 via Ollama |
| Query Cache | Redis |
| Containerization | Docker & Docker Compose |
| Vector Storage | mmap-backed binary file |

---

## Project Structure

```
cmd/
  api/            # coordinator server (fan-out layer)
  shard/          # per-shard search server

internal/
  embed/          # embedding client + normalization
  shardnode/      # shard HTTP handlers and hybrid search
  data/           # shard indexes and vector files

indexer/          # offline indexing pipeline
```

---

## Indexing Pipeline

The offline indexer performs:

1. Wiki JSON ingestion
2. MiniLM embedding generation
3. Vector normalization and mmap write
4. Consistent hash routing to shards
5. Bleve batch indexing
6. Sequential shard-local ID assignment for direct vector lookup

> Document and query vectors are L2-normalized so cosine similarity can be computed efficiently via dot product.

---

## Scoring Strategy

Each shard performs:

1. BM25 candidate retrieval
2. Dense vector similarity (cosine via dot product on BM25 top-K candidates only)
3. Score normalization
4. Hybrid fusion:

```
final_score = 0.7 * BM25_norm + 0.3 * cosine_norm
```

> Cosine reranking runs only over BM25 top-K candidates, not the full vector store — keeping rerank cost constant regardless of shard size.

---

## Query Performance Metrics

Measurements taken on a local multi-shard deployment with parallel coordinator fan-out, 300k documents across 4 shards.

### Sequential Latency (single client, cache warming)

> Measured under sequential single-client load (curl), showing per-query
> latency as the cache warms up from cold start.

| Scenario | Latency |
|--------|--------|
| Redis cache hit | **~250–800 µs** |
| Warm query (steady state) | **~27–37ms** |
| Early queries (cache warming) | **~70–270ms** |
| First query after startup | **~200ms** |

> Benchmarked on a single host where the embedding service and shard servers share resources. In a production deployment, these would run on separate nodes, reducing fanout latency further.

### Latency Under Sustained Load (wrk2, 10k req/sec constant rate)

> Measured using [`wrk2`](https://github.com/giltene/wrk2) — a constant-rate load
> generator that avoids coordinated omission, giving accurate latency percentiles
> under sustained load. Queries drawn from a **Zipfian distribution** over 100
> semantically diverse queries to simulate realistic search traffic patterns.

```bash
./wrk -t8 -c50 -d30s -R 10000 --latency -s search.lua http://localhost:8080/search
```

| Percentile | Cold (Redis flushed) | Warm (Redis populated) |
|---|---|---|
| p50 | 1.47ms | 1.35ms |
| p75 | 2.06ms | 1.81ms |
| p90 | 27ms | 2.27ms |
| p95 | ~80ms | ~2.65ms |
| p99 | 704ms | 4.01ms |
| p99.9 | 780ms | 7.56ms |

> **Cold** = Redis flushed before run; mmap vector pages not yet resident in OS page cache.
> **Warm** = immediate re-run with Redis populated and vector pages hot in page cache.

#### Observations

- **Redis reduces p90 from 27ms → 2.3ms and p99 from 704ms → 4ms** at 10k sustained req/sec under Zipfian load — the dominant performance lever once the cache is warm.
- **Cold p90 of 27ms** reflects Ollama embedding latency (~20–30ms) on cache misses before Redis warms up. Once the top Zipfian queries are cached (within the first few seconds), the embedding path is bypassed entirely.
- **Warm p99 of 4ms** represents the tail of cache misses hitting the full BM25 + rerank pipeline with mmap vector pages already resident in the OS page cache.
- The two caching layers are complementary: **Redis** eliminates repeated pipeline execution; **Linux page cache** eliminates disk I/O for vector reads on cache misses.

### Why cold queries are slower

When a query is executed for the first time after startup, two things happen simultaneously:

1. Redis has no cached results — every query hits the full search pipeline including Ollama embedding (~20–30ms).
2. The memory-mapped vector files may not yet be present in the Linux page cache, triggering page faults as the OS loads vector pages from disk.

Once the top Zipfian queries populate Redis and vector pages warm up in the OS page cache, both costs disappear — subsequent queries either hit Redis directly (~1–2ms) or execute BM25 + rerank with zero-copy mmap reads ~3–5ms.

For queries that are similar but not identical — such as "microsoft" followed by "microsoft stocks" — Redis provides no cache benefit, but the relevant vector pages are likely already warm in the OS page cache, since semantically related documents tend to occupy nearby offsets in the mmap file.

### Latency Breakdown (warm, single host)

| Component | Time |
|---|---|
| Redis cache hit | ~1–2ms |
| Embedding (model warm, cache miss) | ~20–30ms |
| BM25 + fanout + rerank (cache miss) | ~7–10ms |
| **Cache miss total** | **~27–37ms** |

### Query Execution Breakdown

1. Redis cache lookup
2. Query embedding generation (MiniLM via Ollama) *(cache miss only)*
3. Coordinator parallel fan-out to shards
4. BM25 candidate retrieval
5. Dense vector similarity scoring using mmap-backed vectors
6. Hybrid score fusion
7. Top-K merge
8. Cache result in Redis

### Caching Behavior

Turbo Query uses two complementary caching layers:

- **Redis query cache** — stores full query results to accelerate exact repeated queries. Primary performance lever under Zipfian load — reduces p90 by ~12x once warm.
- **Linux page cache** — automatically caches memory-mapped vector pages after first access, eliminating disk I/O for vector reads on cache misses.

Under Zipfian query distributions (realistic search traffic), Redis absorbs the majority of load within seconds of startup. The remaining cache misses execute the full pipeline with mmap-backed vector reads that are zero-copy once pages are resident.

### Notes

- Shard queries are executed **in parallel**, so total latency approximates the **slowest shard**.
- `mmap` allows zero-copy vector access directly from OS page cache — vectors are never deserialized into heap memory.
- Warm queries benefit from the **Linux page cache**, eliminating disk access entirely.
- Cached queries bypass the entire search pipeline via **Redis**, achieving sub-millisecond response times for exact repeated queries.
- Shard document distribution varies based on FNV hash of document IDs — minor imbalance (~10–15%) is expected and does not affect correctness.

---

## Querying the API

Once the coordinator and shard servers are running, queries can be sent to the coordinator endpoint.

### Example Request
```bash
curl -X POST http://localhost:8080/search \
  -H "Content-Type: application/json" \
  -d '{"query":"microsoft","top_k":5}'
```

### Pretty Printed Output
```bash
curl -s -X POST http://localhost:8080/search \
  -H "Content-Type: application/json" \
  -d '{"query":"cricket","top_k":5}' \
  | python -m json.tool
```

### Example Response
```json
[
  {
    "doc_id": "26277",
    "score": 0.9259,
    "shard_id": "3",
    "title": "Microsoft Flight Simulator",
    "text": "Microsoft Flight Simulator is a series of flight simulation video games..."
  },
  {
    "doc_id": "20140",
    "score": 0.9179,
    "shard_id": "0",
    "title": "Microsoft FrontPage",
    "text": "Microsoft FrontPage is a discontinued WYSIWYG HTML editor..."
  }
]
```

---