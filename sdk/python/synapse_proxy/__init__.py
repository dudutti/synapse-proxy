"""Synapse Proxy Python SDK.

Drop-in OpenAI-compatible client for synapse-proxy.com, with extensions
for session recording, cache statistics, savings reports, and A/B benchmark.
"""

from .client import SynapseProxy
from .exceptions import SynapseProxyError, AuthenticationError, APIError
from .types import (
    ChatCompletion,
    SessionRecording,
    CacheStats,
    SavingsReport,
    BenchmarkResult,
)

__version__ = "0.1.0"

__all__ = [
    "SynapseProxy",
    "SynapseProxyError",
    "AuthenticationError",
    "APIError",
    "ChatCompletion",
    "SessionRecording",
    "CacheStats",
    "SavingsReport",
    "BenchmarkResult",
    "__version__",
]
