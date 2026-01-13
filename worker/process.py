# worker/process.py
import sys, json
import numpy as np
import tensorflow as tf
from tensorflow.keras.applications import mobilenet_v2 as mobilenet
from tensorflow.keras.preprocessing import image
from tensorflow.keras.applications.mobilenet_v2 import preprocess_input, decode_predictions

def analyze_image(image_file):
    img = image.load_img(image_file, target_size=(224,224))
    img_array = image.img_to_array(img)
    img_array = np.expand_dims(img_array, axis=0) 
    img_preprocessed = preprocess_input(img_array)

    model = mobilenet.MobileNetV2(weights="imagenet")

    predictions = model.predict(img_preprocessed)
    decoded = decode_predictions(predictions, top=3)[0]
    
    return [{"label": label, "description": desc, "confidence": float(prob)} for (label, desc, prob) in decoded]

if __name__ == "__main__":
    if len(sys.argv) < 2:
        print("Usage: python process.py <path_to_image>")
        sys.exit(1)
    
    image_file = sys.argv[1]
    try:
        analysis_results = analyze_image(image_file)
        print(json.dumps(analysis_results, indent=2))
    except Exception as e:
        print(f"Error processi2ng image: {e}", file = sys.stderr)
        sys.exit(1)



