"""SynapseProxy client.

A thin, typed wrapper over the OpenAI Python SDK that:
- Pre-configures the base URL to a Synapse Proxy instance.
- Re-exports the standard `chat`, `models`, `completions` namespaces.
- Adds Synapse-specific extensions under `client.sessions`, `client.cache`,
  `client.savings`, `client.benchmark`.

The standard OpenAI client is used under the hood for chat completions,
so streaming, function calling, vision, etc. all work out of the box.
"""

from __future__ import annotations

import os
from typing import Any, Dict, Iterable, List, Optional, Union

try:
    # The OpenAI SDK is the only required peer dep.
    from openai import OpenAI as _OpenAI
    from openai import NOT_GIVEN as _NOT_GIVEN
    from openai import NotGiven as _NotGiven
except ImportError as e:
    raise ImportError(
        "The openai package is required: pip install 'synapse-proxy[openai]'"
    ) from e

from .exceptions import APIError, AuthenticationError, SynapseProxyError
from .types import (
    BenchmarkResult,
    CacheStats,
    ChatCompletion,
    SavingsReport,
    SessionRecording,
)


_DEFAULT_BASE_URL = "https://synapse-proxy.com/v1"


class SynapseProxy:
    """Drop-in OpenAI-compatible client for Synapse Proxy.

    Parameters
    ----------
    api_key
        A virtual key starting with `sk-opti-` (or `sk-ant-`, `sk-` for raw
        provider keys if you've stored them in the dashboard).
    base_url
        Override the default Synapse Proxy endpoint. Useful for self-hosted
        instances or staging environments.
    timeout
        Request timeout in seconds. Default 60.
    max_retries
        Number of retries for transient failures. Default 2.
    organization, project
        Forwarded to the underlying OpenAI client for header propagation.
    **openai_kwargs
        Any extra keyword argument is forwarded to `openai.OpenAI`. Useful
        for `http_client`, `default_headers`, etc.
    """

    def __init__(
        self,
        api_key: Optional[str] = None,
        *,
        base_url: Optional[str] = None,
        timeout: float = 60.0,
        max_retries: int = 2,
        organization: Optional[str] = None,
        project: Optional[str] = None,
        **openai_kwargs: Any,
    ) -> None:
        # Resolve API key from explicit arg, then SYNAPSE_PROXY_API_KEY env.
        key = api_key or os.environ.get("SYNAPSE_PROXY_API_KEY")
        if not key:
            raise AuthenticationError(
                "No API key provided. Pass `api_key=` or set the "
                "SYNAPSE_PROXY_API_KEY environment variable."
            )
        # Resolve base URL similarly.
        url = base_url or os.environ.get("SYNAPSE_PROXY_BASE_URL") or _DEFAULT_BASE_URL

        self._openai = _OpenAI(
            api_key=key,
            base_url=url,
            timeout=timeout,
            max_retries=max_retries,
            organization=organization,
            project=project,
            **openai_kwargs,
        )
        self._base_url = url.rstrip("/")
        self.api_key = key

    # ------------------------------------------------------------------
    # Public namespaces (mirror openai.OpenAI)
    # ------------------------------------------------------------------
    @property
    def chat(self):
        return self._openai.chat

    @property
    def completions(self):
        return self._openai.completions

    @property
    def embeddings(self):
        return self._openai.embeddings

    @property
    def models(self):
        return self._openai.models

    # ------------------------------------------------------------------
    # Synapse-specific extensions
    # ------------------------------------------------------------------
    @property
    def sessions(self) -> "SessionsAPI":
        return SessionsAPI(self._base_url, self.api_key)

    @property
    def cache(self) -> "CacheAPI":
        return CacheAPI(self._base_url, self.api_key)

    @property
    def savings(self) -> "SavingsAPI":
        return SavingsAPI(self._base_url, self.api_key)

    @property
    def benchmark(self) -> "BenchmarkAPI":
        return BenchmarkAPI(self._base_url, self.api_key)

    # ------------------------------------------------------------------
    # Convenience helpers
    # ------------------------------------------------------------------
    def complete(
        self,
        model: str,
        messages: Iterable[Dict[str, str]],
        **kwargs: Any,
    ) -> ChatCompletion:
        """One-shot chat completion that returns a typed ChatCompletion.

        Equivalent to `client.chat.completions.create(...)` but returns the
        Synapse `ChatCompletion` dataclass (which includes the `cache_level`
        field populated from the proxy's response headers).
        """
        resp = self._openai.chat.completions.create(
            model=model,
            messages=list(messages),
            **kwargs,
        )
        # The OpenAI SDK exposes the raw response as `model_dump()` or via
        # `to_dict()`. The cache_level lives in a custom header — we read
        # it back through the underlying httpx response.
        raw: Dict[str, Any] = getattr(resp, "to_dict", lambda: {})() or {}
        # Cache level is set by the proxy on `X-SynapseProxy-Cache`. We
        # can't read it from the openai response object directly, so we
        # re-parse from the raw httpx response if available.
        if hasattr(resp, "_raw_response"):
            try:
                raw["cache_level"] = (
                    resp._raw_response.headers.get("X-SynapseProxy-Cache") or ""
                )
                raw["usage"]["cached_tokens"] = int(
                    resp._raw_response.headers.get("X-SynapseProxy-Tokens-Saved") or 0
                )
            except Exception:
                pass
        return ChatCompletion.from_api(raw)

    def __repr__(self) -> str:
        return f"SynapseProxy(base_url={self._base_url!r})"


# ---------------------------------------------------------------------------
# Internal helper: typed HTTP wrapper for Synapse REST extensions
# ---------------------------------------------------------------------------

def _request(
    method: str,
    url: str,
    api_key: str,
    *,
    json: Optional[Dict[str, Any]] = None,
    params: Optional[Dict[str, Any]] = None,
) -> Any:
    import json as _json
    import urllib.error
    import urllib.request

    data = _json.dumps(json).encode("utf-8") if json is not None else None
    full = f"{url}{('?' + urllib.parse.urlencode(params)) if params else ''}"
    req = urllib.request.Request(
        full,
        data=data,
        method=method,
        headers={
            "Content-Type": "application/json",
            "Authorization": f"Bearer {api_key}",
        },
    )
    try:
        with urllib.request.urlopen(req, timeout=60) as resp:
            body = resp.read().decode("utf-8")
            return _json.loads(body) if body else {}
    except urllib.error.HTTPError as e:
        body_raw = e.read().decode("utf-8", errors="replace")
        try:
            body_json = _json.loads(body_raw)
        except Exception:
            body_json = {"raw": body_raw}
        if e.code in (401, 403):
            raise AuthenticationError(
                f"Authentication failed: {body_json}", status=e.code, body=body_json
            ) from e
        raise APIError(
            f"Synapse Proxy returned {e.code}: {body_json}",
            status=e.code,
            body=body_json,
        ) from e
    except urllib.error.URLError as e:
        raise SynapseProxyError(f"Network error: {e.reason}") from e


def _relative(base_url: str, path: str) -> str:
    # The base URL is /v1 (e.g. https://synapse-proxy.com/v1). The
    # dashboard's analytics API sits at /api/analytics/* on the SAME
    # host, just one path up.
    if base_url.endswith("/v1"):
        return base_url[:-3] + path
    return base_url.rstrip("/") + path


# ---------------------------------------------------------------------------
# Synapse extensions
# ---------------------------------------------------------------------------

class SessionsAPI:
    """Manage recorded sessions (start/stop capture of live traffic)."""

    def __init__(self, base_url: str, api_key: str) -> None:
        self._base_url = base_url
        self._api_key = api_key

    def start(
        self,
        *,
        group_by: str = "agent",
        label: Optional[str] = None,
    ) -> SessionRecording:
        """Start a new recording session."""
        body: Dict[str, Any] = {"group_by": group_by}
        if label:
            body["label"] = label
        data = _request(
            "POST",
            _relative(self._base_url, "/api/sessions/record"),
            self._api_key,
            json=body,
        )
        return SessionRecording.from_api(data)

    def stop(self, session_id: str) -> SessionRecording:
        """Stop a running session and persist its records."""
        data = _request(
            "POST",
            _relative(self._base_url, f"/api/sessions/{session_id}/stop"),
            self._api_key,
            json={},
        )
        return SessionRecording.from_api(data)

    def list(self, limit: int = 20) -> List[SessionRecording]:
        """List the most recent sessions."""
        data = _request(
            "GET",
            _relative(self._base_url, "/api/sessions"),
            self._api_key,
            params={"limit": limit},
        )
        return [SessionRecording.from_api(s) for s in data.get("sessions", [])]


class CacheAPI:
    """Cache hit stats per level (L1 / L2 / L3 / NONE)."""

    def __init__(self, base_url: str, api_key: str) -> None:
        self._base_url = base_url
        self._api_key = api_key

    def stats(self, *, days: int = 7) -> CacheStats:
        """Aggregate cache stats for the last `days` days."""
        data = _request(
            "GET",
            _relative(self._base_url, "/api/analytics/cache"),
            self._api_key,
            params={"days": days},
        )
        return CacheStats.from_api(data)


class SavingsAPI:
    """$ saved reports by class and provider."""

    def __init__(self, base_url: str, api_key: str) -> None:
        self._base_url = base_url
        self._api_key = api_key

    def summary(self, *, days: int = 30) -> SavingsReport:
        """Get the savings report for the last `days` days."""
        data = _request(
            "GET",
            _relative(self._base_url, "/api/analytics/savings"),
            self._api_key,
            params={"days": days},
        )
        return SavingsReport.from_api(data)


class BenchmarkAPI:
    """A/B benchmark between two models, judged by an LLM."""

    def __init__(self, base_url: str, api_key: str) -> None:
        self._base_url = base_url
        self._api_key = api_key

    def run(
        self,
        *,
        models: List[str],
        prompt: str,
        judge_model: str = "gpt-4o-mini",
        runs: int = 5,
    ) -> BenchmarkResult:
        """Run an A/B benchmark.

        `models` must be a list of exactly two model names. The proxy
        queries each `runs` times, forwards the responses to `judge_model`
        for scoring, and returns the winner plus the per-model cache hit
        rate and cost.
        """
        if len(models) != 2:
            raise ValueError("`models` must contain exactly two model names")
        body: Dict[str, Any] = {
            "models": models,
            "prompt": prompt,
            "judge_model": judge_model,
            "runs": runs,
        }
        data = _request(
            "POST",
            _relative(self._base_url, "/api/keys/session-benchmark"),
            self._api_key,
            json=body,
        )
        return BenchmarkResult.from_api(data)
