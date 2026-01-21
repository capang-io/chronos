# Chronos - Design Document

## Overview

**Chronos** is a Go application for high-volume asynchronous job processing with state tracking. It allows submitting large batches of records in NDJSON format, processing them in parallel via a worker pool, and retrieving results from a Redis cache.

**Primary Use Case**: Process huge amounts of data asynchronously by sending them to a remote endpoint and tracking their completion status.

---

## General Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                    HTTP Server (main.go)                     │
│  Port: 8080 (configurable via env var PORT)                │
└─────────────┬──────────────────────────────────────────────┘
              │
       ┌──────┴────────┬──────────────┬─────────────┐
       │               │              │             │
    /run          /status          /info      (Routes)
  (POST)          (GET)            (GET)
       │               │              │
┌─────▼──────────┐     │         ┌────▼──────┐
│  JobHandler    │     │         │   Info    │
│                │     │         │  Endpoint │
│ - HandleRun    │     │         └───────────┘
│ - HandleStatus │     │
│ - HandleInfo   │     │
└────────────────┘     │
       │               │
       │         ┌─────▼────────────┐
       │         │    Redis Cache   │
       │         │  (Status Storage)│
       │         └──────────────────┘
       │
┌──────▼────────────────────────────┐
│     Runner & Worker Pool          │
│                                   │
│  ┌─────────────────────────────┐  │
│  │  Records Channel            │  │
│  │  (NDJSON lines buffered)    │  │
│  └──────────────────┬──────────┘  │
│                     │             │
│  ┌──────────────────▼──────────┐  │
│  │  Consumer Pool (N workers)  │  │
│  │  - Consumer 1               │  │
│  │  - Consumer 2               │  │
│  │  - ...                      │  │
│  │  - Consumer N (default: 4)  │  │
│  └──────────────────┬──────────┘  │
│                     │             │
│  ┌──────────────────▼──────────┐  │
│  │  Status Channel             │  │
│  │  (Results from consumers)   │  │
│  └──────────────────┬──────────┘  │
│                     │             │
│  ┌──────────────────▼──────────┐  │
│  │  Cache Listener             │  │
│  │  (Persists to Redis)        │  │
│  └─────────────────────────────┘  │
└───────────────────────────────────┘
```

#### Endpoints:

- **POST /run**
  - Accept: `application/x-ndjson` or `application/jsonl`
  - Flow:
    1. Validates Content-Type
    2. Generates a unique UUID for the job (`job_id`)
    3. Saves NDJSON payload to a temporary file
    4. Starts background processing (`go h.asyncRunJob()`)
    5. Returns 202 Accepted with job_id in `X-Job-ID` header
  - Response:
    ```json
    {
      "message": "Job accepted",
      "job_id": "<uuid>"
    }
    ```

- **GET /status?key=<job_id>**
  - Retrieves job status from Redis cache
  - Reads from `h.cache.GetStats(key)`
  - Response: Completion statistics (count, min_time, max_time)

- **GET /info**
  - Health check/System info endpoint
  - Response: Application status

---

## Example NDJSON Input File

Below is an example of a valid NDJSON file that can be submitted to the `/run` endpoint:

```json
{"primarykey":"batch-001","protocol":"https","host":"api.example.com","port":"443","path":"/webhooks/process","metadata":[{"key":"tenant_id","value":"tenant-123"},{"key":"env","value":"production"}]}
{"id":1,"payload":"{\"user_id\":\"user_001\",\"action\":\"purchase\",\"amount\":99.99}"}
{"id":2,"payload":"{\"user_id\":\"user_002\",\"action\":\"review\",\"rating\":5}"}
{"id":3,"payload":"{\"user_id\":\"user_003\",\"action\":\"signup\",\"email\":\"user3@example.com\"}"}
{"id":4,"payload":"{\"user_id\":\"user_004\",\"action\":\"login\",\"timestamp\":\"2026-01-21T10:30:00Z\"}"}
```

**File Structure:**
- **Line 1 (Configuration)**: Contains protocol, host, port, path, and metadata
  - `primarykey`: Unique identifier for this batch (used as Redis key prefix)
  - `protocol`: `http` or `https`
  - `host`: Target API hostname
  - `port`: Target API port
  - `path`: Target API endpoint path
  - `metadata`: Optional key-value pairs for context

- **Lines 2-N (Records)**: Each line is a JSON object with:
  - `id`: Unique record identifier within the batch
  - `payload`: JSON string containing the actual data to send to the target API

**Submission Example:**
```bash
curl -X POST http://localhost:8080/run \
  -H "Content-Type: application/x-ndjson" \
  --data-binary @input.ndjson
```

**Response:**
```json
{
  "message": "Job accepted",
  "job_id": "job-<uuid>"
}
```

**Status Check:**
```bash
curl http://localhost:8080/status?key=batch-001
```
