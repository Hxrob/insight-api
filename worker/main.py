import os
import time
import json
import boto3
import process  # Imports your existing process.py logic

APP_ENV = os.environ.get("APP_ENV", "production")
SQS_QUEUE_URL = os.environ.get("SQS_QUEUE_URL") # <--- NOW DEFINED
S3_RESULTS_BUCKET = os.environ.get("S3_RESULTS_BUCKET") # <--- NOW DEFINED
AWS_REGION = os.environ.get("AWS_REGION", "us-east-1")

APP_ENV = os.environ.get("APP_ENV", "production")

if APP_ENV == "local":
    print("🔧 Worker running in LOCAL mode")
    s3 = boto3.client('s3',
        region_name=AWS_REGION,
        endpoint_url=S3_ENDPOINT,
        aws_access_key_id=AWS_ACCESS_KEY,
        aws_secret_access_key=AWS_SECRET_KEY
    )
    sqs = boto3.client('sqs',
        region_name=AWS_REGION,
        endpoint_url=SQS_ENDPOINT,
        aws_access_key_id=AWS_ACCESS_KEY,
        aws_secret_access_key=AWS_SECRET_KEY
    )
else:
    print("☁️  Worker running in PRODUCTION mode")
    # In AWS, Boto3 finds creds automatically via IAM
    s3 = boto3.client('s3', region_name=AWS_REGION)
    sqs = boto3.client('sqs', region_name=AWS_REGION)


# --- CLIENT SETUP ---

def process_message(message):
    try:
        body = json.loads(message['Body'])
        job_id = body['job_id']
        s3_path = body['s3_path'] # e.g. "uploads-bucket/uuid.jpg"
        
        print(f"🚀 Processing Job: {job_id}")

        # 1. Download image from S3
        bucket, key = s3_path.split('/', 1)
        local_filename = f"/tmp/{job_id}.jpg"
        s3.download_file(bucket, key, local_filename)

        # 2. Run the ML Analysis (using your existing process.py logic)
        # We call the function directly
        results = process.analyze_image(local_filename)

        # 3. Upload results to S3 (results-bucket)
        results_key = f"{job_id}.json"
        s3.put_object(
            Bucket=S3_RESULTS_BUCKET, 
            Key=results_key,
            Body=json.dumps(results),
            ContentType="application/json"
        )
        print(f"✅ Finished Job: {job_id}")

        # 4. Clean up local file
        os.remove(local_filename)
        return True

    except Exception as e:
        print(f"❌ Error processing job: {e}")
        return False

def poll_queue():
    print("👀 Worker started. Listening for messages...")
    while True:
        try:
            # Ask SQS for messages
            response = sqs.receive_message(
                QueueUrl=SQS_QUEUE_URL,
                MaxNumberOfMessages=1,
                WaitTimeSeconds=10 # Long polling
            )

            if 'Messages' in response:
                for msg in response['Messages']:
                    success = process_message(msg)
                    if success:
                        # If successful, delete message from queue so no one else processes it
                        sqs.delete_message(
                            QueueUrl=SQS_QUEUE_URL,
                            ReceiptHandle=msg['ReceiptHandle']
                        )
            else:
                # No messages, just loop again
                pass
                
        except Exception as e:
            print(f"⚠️ Polling error: {e}")
            time.sleep(5)

if __name__ == "__main__":
    # Ensure buckets exist (retry loop in case MinIO is slow to start)
    # In production, Infrastructure as Code handles this.
    poll_queue()