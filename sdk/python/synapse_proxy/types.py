"""Type definitions for Synapse Proxy SDK.

Mirror the dashboard's API response shapes so users can iterate on
typed objects instead of dictionaries.
"""

from __future__ import annotations

from dataclasses import dataclass, field
from typing import Any, Dict, List, Optional


# ---------------------------------------------------------------------------
# OpenAI-compatible chat completion (subset, just what we expose)
# ---------------------------------------------------------------------------

@dataclass
class ChatMessage:
    role: str
    content: str
    name: Optional[str] = None


@dataclass
class ChatCompletionUsage:
    prompt_tokens: int = 0
    completion_tokens: int = 0
    total_tokens: int = 0
    cached_tokens: int = 0  # Synapse extension: tokens served from provider cache


@dataclass
class ChatCompletionChoice:
    index: int = 0
    message: ChatMessage = field(default_factory=lambda: ChatMessage(role="assistant", content=""))
    finish_reason: str = "stop"


@dataclass
class ChatCompletion:
    id: str = ""
    model: str = ""
    choices: List[ChatCompletionChoice] = field(default_factory=list)
    usage: ChatCompletionUsage = field(default_factory=ChatCompletionUsage)
    # Synapse extension: the cache level that served this request (L1/L2/L3/NONE)
    cache_level: str = ""

    @classmethod
    def from_api(cls, data: Dict[str, Any]) -> "ChatCompletion":
        choices = [
            ChatCompletionChoice(
                index=c.get("index", 0),
                message=ChatMessage(
                    role=c.get("message", {}).get("role", "assistant"),
                    content=c.get("message", {}).get("content", ""),
                ),
                finish_reason=c.get("finish_reason", "stop"),
            )
            for c in data.get("choices", [])
        ]
        usage_data = data.get("usage", {})
        usage = ChatCompletionUsage(
            prompt_tokens=usage_data.get("prompt_tokens", 0),
            completion_tokens=usage_data.get("completion_tokens", 0),
            total_tokens=usage_data.get("total_tokens", 0),
            cached_tokens=usage_data.get("cached_tokens", 0),
        )
        return cls(
            id=data.get("id", ""),
            model=data.get("model", ""),
            choices=choices,
            usage=usage,
            cache_level=data.get("cache_level", ""),
        )


# ---------------------------------------------------------------------------
# Synapse extensions
# ---------------------------------------------------------------------------

@dataclass
class SessionRecording:
    """A session that captures a slice of live traffic for later analysis."""

    id: str
    group_by: str
    started_at: str
    stopped_at: Optional[str] = None
    record_count: int = 0
    tokens_saved: int = 0
    estimated_cost_saved: float = 0.0

    @classmethod
    def from_api(cls, data: Dict[str, Any]) -> "SessionRecording":
        return cls(
            id=data.get("id", ""),
            group_by=data.get("group_by", "agent"),
            started_at=data.get("started_at", ""),
            stopped_at=data.get("stopped_at"),
            record_count=data.get("record_count", 0),
            tokens_saved=data.get("tokens_saved", 0),
            estimated_cost_saved=data.get("estimated_cost_saved", 0.0),
        )


@dataclass
class CacheLevelBreakdown:
    cache_level: str  # L1, L2, L3, NONE
    hits: int
    total: int

    @property
    def hit_rate(self) -> float:
        return (self.hits / self.total) if self.total else 0.0


@dataclass
class CacheStats:
    total_requests: int
    total_tokens_saved: int
    total_cost_saved: float
    by_level: List[CacheLevelBreakdown]
    window_days: int

    @classmethod
    def from_api(cls, data: Dict[str, Any]) -> "CacheStats":
        levels = [
            CacheLevelBreakdown(
                cache_level=item.get("cache_level", "NONE"),
                hits=item.get("hits", 0),
                total=item.get("total", 0),
            )
            for item in data.get("by_level", [])
        ]
        return cls(
            total_requests=data.get("total_requests", 0),
            total_tokens_saved=data.get("total_tokens_saved", 0),
            total_cost_saved=data.get("total_cost_saved", 0.0),
            by_level=levels,
            window_days=data.get("window_days", 7),
        )


@dataclass
class SavingsReport:
    """Aggregated $ saved over a time window."""

    total_cost_saved: float
    total_tokens_saved: int
    total_real_cost: float
    total_optimized_cost: float
    by_class: Dict[str, float]  # InputFresh, CacheRead, CacheCreation, Output
    by_provider: Dict[str, float]
    window_days: int

    @classmethod
    def from_api(cls, data: Dict[str, Any]) -> "SavingsReport":
        return cls(
            total_cost_saved=data.get("total_cost_saved", 0.0),
            total_tokens_saved=data.get("total_tokens_saved", 0),
            total_real_cost=data.get("total_real_cost", 0.0),
            total_optimized_cost=data.get("total_optimized_cost", 0.0),
            by_class=data.get("by_class", {}),
            by_provider=data.get("by_provider", {}),
            window_days=data.get("window_days", 30),
        )


@dataclass
class BenchmarkResult:
    """A/B benchmark winner picked by an LLM judge."""

    winner: str
    model_a: str
    model_b: str
    score_a: float
    score_b: float
    judge_model: str
    judge_reason: str
    cache_hit_rate_a: float = 0.0
    cache_hit_rate_b: float = 0.0
    cost_a: float = 0.0
    cost_b: float = 0.0

    @classmethod
    def from_api(cls, data: Dict[str, Any]) -> "BenchmarkResult":
        return cls(
            winner=data.get("winner", ""),
            model_a=data.get("model_a", ""),
            model_b=data.get("model_b", ""),
            score_a=data.get("score_a", 0.0),
            score_b=data.get("score_b", 0.0),
            judge_model=data.get("judge_model", ""),
            judge_reason=data.get("judge_reason", ""),
            cache_hit_rate_a=data.get("cache_hit_rate_a", 0.0),
            cache_hit_rate_b=data.get("cache_hit_rate_b", 0.0),
            cost_a=data.get("cost_a", 0.0),
            cost_b=data.get("cost_b", 0.0),
        )
