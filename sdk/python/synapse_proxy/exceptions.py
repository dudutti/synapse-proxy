"""Exception hierarchy for Synapse Proxy SDK."""


class SynapseProxyError(Exception):
    """Base exception for all Synapse Proxy SDK errors."""

    def __init__(self, message: str, status: int | None = None, body: object | None = None):
        super().__init__(message)
        self.status = status
        self.body = body


class AuthenticationError(SynapseProxyError):
    """Raised when the API key is missing, invalid, or revoked (HTTP 401/403)."""


class APIError(SynapseProxyError):
    """Raised when the proxy returns a non-2xx response other than auth errors."""


class ConnectionError(SynapseProxyError):
    """Raised when the SDK cannot reach the proxy (DNS, TCP, TLS)."""
