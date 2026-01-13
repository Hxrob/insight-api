# Insight API

A distributed image analysis service that uses machine learning to classify and analyze images. The system follows an asynchronous, microservices architecture with a Go-based API server and a Python-based worker service that processes images using TensorFlow's MobileNetV2 model.

## Architecture Overview

The project implements a **producer-consumer pattern** using AWS-compatible services:

```
Client → API (Go) → S3 (MinIO) → SQS (ElasticMQ) → Worker (Python) → Results (S3)
```

### Components

1. **API Service** (`api/`): A Go HTTP server that handles image uploads and result retrieval
2. **Worker Service** (`worker/`): A Python service that processes images using TensorFlow
3. **Storage** (MinIO): S3-compatible object storage for images and results
4. **Queue** (ElasticMQ): SQS-compatible message queue for job distribution

## How It Works

### 1. Image Upload
- Client sends a POST request to `/upload` with an image file
- API generates a unique job ID and uploads the image to S3
- API sends a job message to SQS with the job ID and S3 path
- API immediately returns the job ID to the client (202 Accepted)

### 2. Image Processing
- Worker continuously polls SQS for new messages
- When a message is received, the worker:
  - Downloads the image from S3
  - Processes it using MobileNetV2 (ImageNet pre-trained model)
  - Generates top 3 predictions with confidence scores
  - Uploads results as JSON to the results bucket
  - Deletes the message from the queue

### 3. Result Retrieval
- Client polls `/results/{job_id}` to check job status
- Returns `{"status": "pending"}` if results aren't ready yet
- Returns the full JSON results when processing is complete

## Technology Stack

### API Service
- **Language**: Go 1.25.4
- **Framework**: Standard `net/http`
- **AWS SDK**: `aws-sdk-go-v2` for S3 and SQS integration
- **Features**: 
  - Multipart file upload handling
  - UUID-based job tracking
  - Health check endpoint

### Worker Service
- **Language**: Python
- **ML Framework**: TensorFlow with Keras
- **Model**: MobileNetV2 (ImageNet pre-trained)
- **AWS SDK**: Boto3 for S3 and SQS integration
- **Features**:
  - Long polling for efficient message retrieval
  - Automatic error handling and retry logic
  - Local file cleanup after processing

### Infrastructure
- **Storage**: MinIO (S3-compatible) for local development
- **Queue**: ElasticMQ (SQS-compatible) for local development
- **Orchestration**: Docker Compose for local development

## Local Development

### Prerequisites
- Docker and Docker Compose
- Go 1.25.4+ (for local API development)
- Python 3.12+ (for local worker development)

### Repository Setup

If you're cloning this repository, you'll need to set up the Python virtual environment:

```bash
cd worker
python3 -m venv venv
source venv/bin/activate  # On Windows: venv\Scripts\activate
pip install -r requirements.txt
```

For local development without Docker, you can test the worker directly:
```bash
python process.py path/to/image.jpg
```

### Running the Stack

1. **Start all services**:
   ```bash
   docker-compose up --build
   ```

2. **Services will be available at**:
   - API: `http://localhost:8080`
   - MinIO Console: `http://localhost:9001` (minioadmin/minioadmin)
   - ElasticMQ UI: `http://localhost:9325`

3. **Upload an image**:
   ```bash
   curl -X POST http://localhost:8080/upload \
     -F "image=@squirrel.jpg"
   ```

4. **Check results**:
   ```bash
   curl http://localhost:8080/results/{job_id}
   ```

### Environment Variables

The system supports two modes:

**Local Mode** (`APP_ENV=local`):
- Uses MinIO for S3 storage
- Uses ElasticMQ for SQS queue
- Requires explicit credentials (minioadmin/minioadmin)

**Production Mode** (`APP_ENV=production`):
- Uses real AWS S3 and SQS
- Uses IAM roles for authentication (ECS/EKS)
- No explicit credentials needed

## API Endpoints

### `POST /upload`
Upload an image for analysis.

**Request**:
- Content-Type: `multipart/form-data`
- Field name: `image`
- Max file size: 10MB

**Response** (202 Accepted):
```json
{
  "job_id": "550e8400-e29b-41d4-a716-446655440000"
}
```

### `GET /results/{job_id}`
Retrieve analysis results for a job.

**Response** (200 OK - Pending):
```json
{
  "status": "pending"
}
```

**Response** (200 OK - Complete):
```json
[
  {
    "label": "n02119789",
    "description": "kit_fox",
    "confidence": 0.823456
  },
  {
    "label": "n02119022",
    "description": "red_fox",
    "confidence": 0.123456
  },
  {
    "label": "n02120505",
    "description": "grey_fox",
    "confidence": 0.053456
  }
]
```

### `GET /health`
Health check endpoint.

**Response** (200 OK):
```
Service is healthy and ready to process requests
```

## Project Structure

```
insight-api/
├── api/
│   ├── main.go          # Go API server
│   ├── Dockerfile       # API container definition
│   ├── go.mod           # Go dependencies
│   └── go.sum           # Go dependency checksums
├── worker/
│   ├── main.py          # Worker service (SQS polling)
│   ├── process.py       # Image analysis logic
│   ├── requirements.txt # Python dependencies
│   └── Dockerfile       # Worker container definition
├── docker-compose.yml   # Local development orchestration
├── elasticmq.conf       # ElasticMQ configuration
├── .gitignore           # Git ignore rules (excludes venv/, etc.)
└── README.md            # This file
```

**Note**: The `worker/venv/` directory is intentionally excluded from version control. Each developer should create their own virtual environment using `requirements.txt`.

## Production Deployment

For production deployment on AWS:

1. **Build and push Docker images** to ECR:
   ```bash
   # Build API image
   docker build -t insight-api:latest ./api
   docker tag insight-api:latest 908027409188.dkr.ecr.us-east-1.amazonaws.com/insight-api:latest
   docker push 908027409188.dkr.ecr.us-east-1.amazonaws.com/insight-api:latest
   
   # Build worker image
   docker build -t insight-worker:latest ./worker
   docker tag insight-worker:latest 908027409188.dkr.ecr.us-east-1.amazonaws.com/insight-worker:latest
   docker push 908027409188.dkr.ecr.us-east-1.amazonaws.com/insight-worker:latest
   ```

2. **Deploy infrastructure**:
   - Create S3 buckets: `uploads-bucket` and `results-bucket`
   - Create SQS queue: `insight-jobs`
   - Deploy API service (ECS/EKS) with IAM role for S3/SQS access
   - Deploy worker service (ECS/EKS) with IAM role for S3/SQS access

3. **Set environment variables**:
   - `APP_ENV=production`
   - `AWS_REGION=us-east-1`
   - `S3_UPLOADS_BUCKET=uploads-bucket`
   - `S3_RESULTS_BUCKET=results-bucket`
   - `SQS_QUEUE_URL=https://sqs.us-east-1.amazonaws.com/ACCOUNT_ID/insight-jobs`

## Features

- ✅ Asynchronous job processing
- ✅ Scalable worker architecture (multiple workers can process jobs in parallel)
- ✅ S3-based storage for images and results
- ✅ Queue-based job distribution
- ✅ Health check endpoint for monitoring
- ✅ Local development environment with Docker Compose
- ✅ Production-ready AWS integration
- ✅ Image classification using state-of-the-art ML model

## License

[Add your license here]

