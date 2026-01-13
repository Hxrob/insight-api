package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/google/uuid"
)

// ---
// CONFIGURATION
// These values are read from environment variables,
// which we will set in our docker-compose.yml file.
// ---
var (
	// The S3-compatible service (MinIO)
	s3Endpoint      = getEnv("S3_ENDPOINT", "http://storage:9000")
	s3Region        = getEnv("AWS_REGION", "us-east-1")
	s3AccessKey     = getEnv("AWS_ACCESS_KEY_ID", "DUMMY") // Dummy credentials for local
	s3SecretKey     = getEnv("AWS_SECRET_ACCESS_KEY", "DUMMY")
	s3UploadBucket  = getEnv("S3_UPLOADS_BUCKET", "uploads-bucket")
	s3ResultsBucket = getEnv("S3_RESULTS_BUCKET", "results-bucket")

	// The SQS-compatible service (ElasticMQ)
	sqsEndpoint = getEnv("SQS_ENDPOINT", "http://queue:9650")
	sqsQueueURL = getEnv("SQS_QUEUE_URL", "http://queue:9650/000000000000/your-insight-jobs")
)

// apiHandler holds the AWS clients our handlers will need.
// This is a good practice called "dependency injection".
type apiHandler struct {
	s3Client  *s3.Client
	sqsClient *sqs.Client
}

func loadAWSConfig(ctx context.Context) (aws.Config, error) {
	// Check if we are in local mode
	appEnv := os.Getenv("APP_ENV")

	if appEnv == "local" {
		log.Println("🔧 Running in LOCAL mode (using MinIO/ElasticMQ)")
		// No need for custom resolver - we'll configure endpoints per-service
		return config.LoadDefaultConfig(ctx,
			config.WithRegion(s3Region),
			config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(s3AccessKey, s3SecretKey, "")),
		)
	}

	// PRODUCTION MODE (AWS)
	// The SDK automatically picks up IAM Role credentials in ECS.
	log.Println("☁️  Running in PRODUCTION mode (using real AWS)")
	return config.LoadDefaultConfig(ctx, config.WithRegion("us-east-1"))
}

func main() {
	ctx := context.TODO()

	// use the new smart loader
	cfg, err := loadAWSConfig(ctx)
	if err != nil {
		log.Fatalf("failed to create AWS config: %v", err)
	}

	// creates the S3 client
	appEnv := os.Getenv("APP_ENV")
	s3Options := []func(*s3.Options){
		func(o *s3.Options) {
			o.UsePathStyle = true // Required for MinIO
			if appEnv == "local" {
				o.BaseEndpoint = aws.String(s3Endpoint)
			}
		},
	}
	s3Client := s3.NewFromConfig(cfg, s3Options...)

	// creates the SQS client
	sqsOptions := []func(*sqs.Options){
		func(o *sqs.Options) {
			if appEnv == "local" {
				o.BaseEndpoint = aws.String(sqsEndpoint)
			}
		},
	}
	sqsClient := sqs.NewFromConfig(cfg, sqsOptions...)

	// 4. Create our handler
	h := &apiHandler{
		s3Client:  s3Client,
		sqsClient: sqsClient,
	}

	// 5. Define our HTTP routes (requires Go 1.22+)
	http.HandleFunc("POST /upload", h.handleUpload)
	http.HandleFunc("GET /results/{job_id}", h.handleGetResult)

	http.HandleFunc("GET /health", healthCheckHandler)
	
	// 6. Start the server
	log.Println("✅ API server starting on :8080")
	if err := http.ListenAndServe(":8080", nil); err != nil {
		log.Fatalf("server failed: %v", err)
	}
}

// Health Check Handler
func healthCheckHandler(w http.ResponseWriter, r *http.Request) {
    w.WriteHeader(http.StatusOK) // Explicitly sends HTTP 200 OK (Status Code 200)
    // The response body is optional, but helpful for debugging
    fmt.Fprintf(w, "Service is healthy and ready to process requests")
}

// ---
// HTTP HANDLERS
// ---

func (h *apiHandler) handleUpload(w http.ResponseWriter, r *http.Request) {
	// 1. Parse the multipart form (max 10MB)
	if err := r.ParseMultipartForm(10 << 20); err != nil {
		respondError(w, http.StatusBadRequest, "Could not parse form")
		return
	}

	// 2. Get the file from the form
	file, header, err := r.FormFile("image")
	if err != nil {
		respondError(w, http.StatusBadRequest, "Form file 'image' is required")
		return
	}
	defer file.Close()

	// 3. Generate a unique job ID
	jobID := uuid.New().String()
	// Use the original file's extension (e.g., .jpg)
	s3Key := fmt.Sprintf("%s%s", jobID, filepath.Ext(header.Filename))

	// 4. Upload the file to S3 (MinIO)
	_, err = h.s3Client.PutObject(r.Context(), &s3.PutObjectInput{
		Bucket: aws.String(s3UploadBucket),
		Key:    aws.String(s3Key),
		Body:   file,
	})
	if err != nil {
		log.Printf("Failed to upload to S3: %v", err)
		respondError(w, http.StatusInternalServerError, "Failed to upload file")
		return
	}

	// 5. Create the job message
	messageBody, _ := json.Marshal(map[string]string{
		"job_id":  jobID,
		"s3_path": fmt.Sprintf("%s/%s", s3UploadBucket, s3Key),
	})

	// 6. Send the job to SQS (ElasticMQ)
	_, err = h.sqsClient.SendMessage(r.Context(), &sqs.SendMessageInput{
		QueueUrl:    aws.String(sqsQueueURL),
		MessageBody: aws.String(string(messageBody)),
	})
	if err != nil {
		log.Printf("Failed to send SQS message: %v", err)
		respondError(w, http.StatusInternalServerError, "Failed to queue job")
		return
	}

	// 7. Return the job ID to the user
	log.Printf("Job created: %s", jobID)
	respondJSON(w, http.StatusAccepted, map[string]string{"job_id": jobID})
}

func (h *apiHandler) handleGetResult(w http.ResponseWriter, r *http.Request) {
	// 1. Get the job_id from the URL path (Go 1.22+ feature)
	jobID := r.PathValue("job_id")
	if jobID == "" {
		respondError(w, http.StatusBadRequest, "Missing job_id")
		return
	}

	s3Key := fmt.Sprintf("%s.json", jobID)

	// 2. Try to get the result file from S3 (MinIO)
	output, err := h.s3Client.GetObject(r.Context(), &s3.GetObjectInput{
		Bucket: aws.String(s3ResultsBucket),
		Key:    aws.String(s3Key),
	})

	// 3. Handle the response
	if err != nil {
		// This is the *expected* error if the file isn't ready.
		var nsk *s3types.NoSuchKey
		if errors.As(err, &nsk) {
			// File not found, so the job is still pending
			respondJSON(w, http.StatusOK, map[string]string{"status": "pending"})
			return
		}
		// This is a *real* error
		log.Printf("Failed to get S3 object: %v", err)
		respondError(w, http.StatusInternalServerError, "Error checking job status")
		return
	}

	// 4. Success! Stream the JSON result directly to the response
	defer output.Body.Close()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	io.Copy(w, output.Body)
}

// ---
// HELPER FUNCTIONS
// ---

// getEnv is a helper to read an environment variable or return a default.
func getEnv(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok {
		return value
	}
	return fallback
}

// respondError sends a standard JSON error message.
func respondError(w http.ResponseWriter, code int, message string) {
	respondJSON(w, code, map[string]string{"error": message})
}

// respondJSON writes a JSON response.
func respondJSON(w http.ResponseWriter, code int, payload interface{}) {
	response, _ := json.Marshal(payload)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	w.Write(response)
}
