"""
upstream_mock.py — Minimal OpenAI-compatible upstream for local
testing. Always returns 200 with a realistic chat completion
response so the proxy can exercise its hook pipeline end-to-end
without burning real tokens.

Usage:
    python upstream_mock.py 9090
"""
import json
import sys
from http.server import BaseHTTPRequestHandler, HTTPServer


class MockOpenAIHandler(BaseHTTPRequestHandler):
    def do_POST(self):
        length = int(self.headers.get("Content-Length", "0"))
        body_raw = self.rfile.read(length) if length else b"{}"
        try:
            body = json.loads(body_raw)
        except Exception:
            body = {}
        print("MOCK UPSTREAM POST body:", json.dumps(body), flush=True)

        content_query = ""
        for msg in body.get("messages", []):
            if msg.get("content"):
                content_query += " " + str(msg.get("content"))
        print("MOCK UPSTREAM content_query:", content_query, flush=True)

        if "TRIGGER_TOOL_CALL:" in content_query:
            parts = content_query.split("TRIGGER_TOOL_CALL:")
            cache_key = parts[1].strip().split()[0]
            response = {
                "id": "chatcmpl-mock-tool",
                "object": "chat.completion",
                "created": 0,
                "model": body.get("model", "gpt-4o-mini"),
                "choices": [
                    {
                        "index": 0,
                        "finish_reason": "tool_calls",
                        "message": {
                            "role": "assistant",
                            "content": None,
                            "tool_calls": [
                                {
                                    "id": "call_mock",
                                    "type": "function",
                                    "function": {
                                        "name": "synapse_retrieve",
                                        "arguments": json.dumps({"cache_key": cache_key})
                                    }
                                }
                            ]
                        }
                    }
                ],
                "usage": {
                    "prompt_tokens": 10,
                    "completion_tokens": 10,
                    "total_tokens": 20
                }
            }
        else:
            response = {
                "id": "chatcmpl-mock",
                "object": "chat.completion",
                "created": 0,
                "model": body.get("model", "gpt-4o-mini"),
                "choices": [
                    {
                        "index": 0,
                        "message": {
                            "role": "assistant",
                            "content": "mock response from upstream_mock.py",
                        },
                        "finish_reason": "stop",
                    }
                ],
                "usage": {
                    "prompt_tokens": 11,
                    "completion_tokens": 22,
                    "total_tokens": 33,
                },
            }

        payload = json.dumps(response).encode("utf-8")
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(payload)))
        self.end_headers()
        self.wfile.write(payload)

    def do_GET(self):
        if self.path == "/v1/models":
            response = {
                "data": [
                    {"id": "MiniMax-M2.7"},
                    {"id": "MiniMax-M3"}
                ]
            }
            payload = json.dumps(response).encode("utf-8")
            self.send_response(200)
            self.send_header("Content-Type", "application/json")
            self.send_header("Content-Length", str(len(payload)))
            self.end_headers()
            self.wfile.write(payload)
        else:
            self.send_response(404)
            self.end_headers()

    def log_message(self, *args, **kwargs):
        return


def main():
    port = int(sys.argv[1]) if len(sys.argv) > 1 else 9090
    server = HTTPServer(("0.0.0.0", port), MockOpenAIHandler)
    print(f"upstream_mock listening on :{port}")
    try:
        server.serve_forever()
    except KeyboardInterrupt:
        server.shutdown()


if __name__ == "__main__":
    main()
