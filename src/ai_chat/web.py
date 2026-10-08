"""Small async client for Ollama Cloud's web search and fetch APIs."""

import asyncio
from typing import Annotated, Any
from urllib.parse import urlsplit

import httpx
from pydantic import BaseModel, ConfigDict, Field, ValidationError

_WEB_SEARCH_URL = "https://ollama.com/api/web_search"
_WEB_FETCH_URL = "https://ollama.com/api/web_fetch"
_REQUEST_TIMEOUT = 30.0
_MAX_RESULTS = 10
_MAX_RESULT_CONTENT = 20_000
_MAX_FETCH_CONTENT = 20_000
_MAX_TITLE = 512
_MAX_URL = 2_048
_MAX_LINKS = 20


class _SearchResult(BaseModel):
    model_config = ConfigDict(extra="ignore")

    title: str
    url: str
    content: str


class _SearchResponse(BaseModel):
    model_config = ConfigDict(extra="ignore")

    results: list[_SearchResult]


class _FetchResponse(BaseModel):
    model_config = ConfigDict(extra="ignore")

    title: str
    content: str
    links: list[str]


class WebTools:
    """Call Ollama's fixed web endpoints using a private API key."""

    def __init__(
        self,
        api_key: str,
        *,
        transport: httpx.AsyncBaseTransport | None = None,
    ) -> None:
        self._api_key = api_key
        self._transport = transport

    async def web_search(
        self,
        query: str,
        max_results: Annotated[int, Field(ge=1, le=10)] = 5,
    ) -> dict[str, Any]:
        """Search the web and return bounded titles, URLs, and snippets."""
        if not isinstance(query, str) or not query.strip():
            return {"error": "A non-empty search query is required."}
        if (
            isinstance(max_results, bool)
            or not isinstance(max_results, int)
            or not 1 <= max_results <= _MAX_RESULTS
        ):
            return {"error": "max_results must be an integer from 1 to 10."}

        response = await self._post(
            _WEB_SEARCH_URL,
            {"query": query.strip(), "max_results": max_results},
        )
        if isinstance(response, dict):
            return response

        results = response.results[:max_results]
        truncated = len(response.results) > len(results)
        content_limit = _MAX_RESULT_CONTENT // len(results) if results else 0
        bounded_results: list[dict[str, str]] = []
        for item in results:
            title = item.title[:_MAX_TITLE]
            url = item.url[:_MAX_URL]
            content = item.content[:content_limit]
            truncated = truncated or (
                len(title) < len(item.title)
                or len(url) < len(item.url)
                or len(content) < len(item.content)
            )
            bounded_results.append({"title": title, "url": url, "content": content})

        return {
            "query": query.strip(),
            "results": bounded_results,
            "truncated": truncated,
        }

    async def web_fetch(self, url: str) -> dict[str, Any]:
        """Fetch a page through Ollama's web API, never directly from its URL."""
        if not self._valid_url(url):
            return {"error": "Enter a valid HTTP or HTTPS URL without credentials."}
        if len(url) > _MAX_URL:
            return {"error": "The URL must be no longer than 2048 characters."}

        response = await self._post(_WEB_FETCH_URL, {"url": url})
        if isinstance(response, dict):
            return response

        title = response.title[:_MAX_TITLE]
        content = response.content[:_MAX_FETCH_CONTENT]
        links = [link[:_MAX_URL] for link in response.links[:_MAX_LINKS]]
        truncated = (
            len(title) < len(response.title)
            or len(content) < len(response.content)
            or len(links) < len(response.links)
            or any(len(link) > _MAX_URL for link in response.links[:_MAX_LINKS])
        )
        return {
            "url": url,
            "title": title,
            "content": content,
            "links": links,
            "truncated": truncated,
        }

    async def _post(
        self, endpoint: str, payload: dict[str, Any]
    ) -> BaseModel | dict[str, str]:
        try:
            async with (
                asyncio.timeout(_REQUEST_TIMEOUT),
                httpx.AsyncClient(
                    timeout=_REQUEST_TIMEOUT,
                    transport=self._transport,
                ) as client,
            ):
                response = await client.post(
                    endpoint,
                    json=payload,
                    headers={"Authorization": f"Bearer {self._api_key}"},
                )
        except (httpx.TimeoutException, TimeoutError):
            return {"error": "The Ollama web request timed out."}
        except httpx.RequestError:
            return {"error": "The Ollama web service could not be reached."}

        if response.status_code in (401, 403):
            return {
                "error": "Ollama rejected web access. Check the API key and permissions."
            }
        if response.status_code == 429:
            return {"error": "Ollama web access is rate limited. Try again later."}
        if not response.is_success:
            return {"error": "The Ollama web request failed."}

        try:
            body = response.json()
        except ValueError:
            return {"error": "Ollama returned an invalid web response."}

        try:
            model = _SearchResponse if endpoint == _WEB_SEARCH_URL else _FetchResponse
            return model.model_validate(body)
        except ValidationError:
            return {"error": "Ollama returned an invalid web response."}

    @staticmethod
    def _valid_url(url: str) -> bool:
        if not isinstance(url, str) or not url:
            return False
        try:
            parsed = urlsplit(url)
            return (
                parsed.scheme.lower() in {"http", "https"}
                and bool(parsed.hostname)
                and parsed.username is None
                and parsed.password is None
            )
        except ValueError:
            return False
