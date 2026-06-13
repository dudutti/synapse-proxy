from fastapi import FastAPI
from pydantic import BaseModel
from sentence_transformers import SentenceTransformer

app = FastAPI()

# Load model globally to keep it warm in memory
model = SentenceTransformer('paraphrase-multilingual-MiniLM-L12-v2')

class EmbedRequest(BaseModel):
    text: str

@app.post("/embed")
async def embed_text(req: EmbedRequest):
    # Calculate embedding
    vector = model.encode(req.text)
    # Convert numpy array to list of floats
    vector_list = vector.tolist()
    return {"vector": vector_list}

@app.get("/health")
async def health():
    return {"status": "ok"}
