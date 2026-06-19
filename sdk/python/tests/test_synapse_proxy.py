"""Tests for the Synapse Proxy Python SDK.

These tests use the `responses` library to mock the dashboard's analytics
API and the OpenAI client. They do not require a running Synapse Proxy.
"""

import json
import pytest
import responses

from synapse_proxy import (
    AuthenticationError,
    APIError,
    BenchmarkResult,
    CacheStats,
    ChatCompletion,
    SavingsReport,
    SessionRecording,
    SynapseProxy,
    SynapseProxyError,
)
from synapse_proxy.client import _relative

DUMMY_KEY = "sk-opti-dummy-key-for-tests"
DUMMY_BASE = "https://synapse-proxy.com/v1"


# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------

@pytest.fixture
def client() -> SynapseProxy:
    return SynapseProxy(api_key=DUMMY_KEY, base_url=DUMMY_BASE, max_retries=0)


# ---------------------------------------------------------------------------
# URL construction
# ---------------------------------------------------------------------------

class TestRelativeUrl:
    def test_strips_v1_and_appends_path(self):
        assert _relative("https://synapse-proxy.com/v1", "/api/foo") == \
            "https://synapse-proxy.com/api/foo"

    def test_handles_no_v1_suffix(self):
        assert _relative("https://synapse-proxy.com", "/api/foo") == \
            "https://synapse-proxy.com/api/foo"

    def test_strips_trailing_slash(self):
        assert _relative("https://synapse-proxy.com/v1/", "/api/foo") == \
            "https://synapse-proxy.com/api/foo"


# ---------------------------------------------------------------------------
# Auth & init
# ---------------------------------------------------------------------------

class TestInit:
    def test_requires_api_key(self, monkeypatch):
        monkeypatch.delenv("SYNAPSE_PROXY_API_KEY", raising=False)
        with pytest.raises(AuthenticationError):
            SynapseProxy()

    def test_reads_api_key_from_env(self, monkeypatch):
        monkeypatch.setenv("SYNAPSE_PROXY_API_KEY", DUMMY_KEY)
        client = SynapseProxy()
        assert client.api_key == DUMMY_KEY

    def test_default_base_url(self, monkeypatch):
        monkeypatch.delenv("SYNAPSE_PROXY_BASE_URL", raising=False)
        c = SynapseProxy(api_key=DUMMY_KEY)
        assert c._base_url == "https://synapse-proxy.com/v1"

    def test_custom_base_url(self):
        c = SynapseProxy(api_key=DUMMY_KEY, base_url="https://staging.example.com/v1")
        assert c._base_url == "https://staging.example.com/v1"


# ---------------------------------------------------------------------------
# Sessions API
# ---------------------------------------------------------------------------

class TestSessions:
    @responses.activate
    def test_start(self, client):
        responses.post(
            "https://synapse-proxy.com/api/sessions/record",
            json={
                "id": "sess-1",
                "group_by": "agent",
                "started_at": "2026-06-18T20:00:00Z",
                "record_count": 0,
                "tokens_saved": 0,
                "estimated_cost_saved": 0.0,
            },
            status=200,
        )
        sess = client.sessions.start(group_by="agent", label="demo")
        assert isinstance(sess, SessionRecording)
        assert sess.id == "sess-1"
        assert sess.group_by == "agent"
        body = json.loads(responses.calls[0].request.body)
        assert body == {"group_by": "agent", "label": "demo"}

    @responses.activate
    def test_stop(self, client):
        responses.post(
            "https://synapse-proxy.com/api/sessions/sess-1/stop",
            json={
                "id": "sess-1",
                "group_by": "agent",
                "started_at": "2026-06-18T20:00:00Z",
                "stopped_at": "2026-06-18T20:30:00Z",
                "record_count": 142,
                "tokens_saved": 12450,
                "estimated_cost_saved": 0.0372,
            },
            status=200,
        )
        sess = client.sessions.stop("sess-1")
        assert sess.stopped_at is not None
        assert sess.record_count == 142

    @responses.activate
    def test_list(self, client):
        responses.get(
            "https://synapse-proxy.com/api/sessions?limit=5",
            json={"sessions": [
                {"id": "sess-1", "group_by": "agent", "started_at": "...",
                 "record_count": 10, "tokens_saved": 100, "estimated_cost_saved": 0.01}
            ]},
            status=200,
        )
        sessions = client.sessions.list(limit=5)
        assert len(sessions) == 1
        assert sessions[0].id == "sess-1"


# ---------------------------------------------------------------------------
# Cache / Savings / Benchmark
# ---------------------------------------------------------------------------

class TestCache:
    @responses.activate
    def test_stats(self, client):
        responses.get(
            "https://synapse-proxy.com/api/analytics/cache?days=7",
            json={
                "total_requests": 1234,
                "total_tokens_saved": 56789,
                "total_cost_saved": 1.234,
                "by_level": [
                    {"cache_level": "L1", "hits": 800, "total": 1234},
                    {"cache_level": "L2", "hits": 200, "total": 1234},
                    {"cache_level": "L3", "hits": 100, "total": 1234},
                    {"cache_level": "NONE", "hits": 0, "total": 1234},
                ],
                "window_days": 7,
            },
            status=200,
        )
        stats = client.cache.stats(days=7)
        assert isinstance(stats, CacheStats)
        assert stats.total_requests == 1234
        assert stats.by_level[0].cache_level == "L1"
        assert stats.by_level[0].hit_rate == pytest.approx(800 / 1234)


class TestSavings:
    @responses.activate
    def test_summary(self, client):
        responses.get(
            "https://synapse-proxy.com/api/analytics/savings?days=30",
            json={
                "total_cost_saved": 12.34,
                "total_tokens_saved": 1234567,
                "total_real_cost": 56.78,
                "total_optimized_cost": 44.44,
                "by_class": {
                    "InputFresh": 1.5,
                    "CacheRead": 8.2,
                    "CacheCreation": 0.34,
                    "Output": 2.3,
                },
                "by_provider": {"minimax": 7.0, "openai": 5.34},
                "window_days": 30,
            },
            status=200,
        )
        report = client.savings.summary(days=30)
        assert isinstance(report, SavingsReport)
        assert report.by_class["CacheRead"] == 8.2
        assert report.by_provider["minimax"] == 7.0


class TestBenchmark:
    @responses.activate
    def test_run(self, client):
        responses.post(
            "https://synapse-proxy.com/api/keys/session-benchmark",
            json={
                "winner": "minimax-m3",
                "model_a": "gpt-4o-mini",
                "model_b": "minimax-m3",
                "score_a": 7.2,
                "score_b": 8.4,
                "judge_model": "gpt-4o-mini",
                "judge_reason": "Better reasoning, less hallucination.",
                "cache_hit_rate_a": 0.42,
                "cache_hit_rate_b": 0.99,
                "cost_a": 0.0123,
                "cost_b": 0.0014,
            },
            status=200,
        )
        result = client.benchmark.run(
            models=["gpt-4o-mini", "minimax-m3"],
            prompt="Explain TCP slow start in 3 sentences.",
            judge_model="gpt-4o-mini",
            runs=3,
        )
        assert isinstance(result, BenchmarkResult)
        assert result.winner == "minimax-m3"
        assert result.cache_hit_rate_b == 0.99

    def test_requires_exactly_two_models(self, client):
        with pytest.raises(ValueError):
            client.benchmark.run(models=["only-one"], prompt="hi")


# ---------------------------------------------------------------------------
# Error handling
# ---------------------------------------------------------------------------

class TestErrorHandling:
    @responses.activate
    def test_401_raises_authentication_error(self, client):
        responses.get(
            "https://synapse-proxy.com/api/analytics/cache?days=7",
            json={"error": "invalid key"},
            status=401,
        )
        with pytest.raises(AuthenticationError) as exc:
            client.cache.stats()
        assert exc.value.status == 401

    @responses.activate
    def test_500_raises_api_error(self, client):
        responses.get(
            "https://synapse-proxy.com/api/analytics/cache?days=7",
            json={"error": "boom"},
            status=500,
        )
        with pytest.raises(APIError) as exc:
            client.cache.stats()
        assert exc.value.status == 500
