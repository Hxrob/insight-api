FROM python:3.9-slim

WORKDIR /app

RUN apt-get update && apt-get install -y \
    libgl1 \
    && rm -rf /var/lib/apt/lists/*

COPY requirements.txt .
RUN pip install --no-cache-dir -r requirements.txt

# Copy BOTH scripts
COPY process.py .
COPY main.py .

# Run the new polling script
CMD ["python", "main.py"]