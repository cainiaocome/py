import asyncio
import json

import httpx
import pytest

from ai_chat.web import WebTools


def _transport(response_data, *, status=200):
    seen = []

    def handler(request):
        seen.append(request)
        return httpx.Response(status, json=response_data)

    return httpx.MockTransport(handler), seen


async def test_search_posts_to_fixed_endpoint_with_bearer_key_and_bounds_results():
    transport, seen = _transport(
        {
            "results": [
                {
                    "title": "T" * 600,
                    "url": "https://example.com/" + "u" * 2100,
                    "content": "C" * 15_000,
                },
                {
                    "title": "Second",
                    "url": "https://example.org",
                    "content": "D" * 15_000,
                },
                {"title": "Dropped", "url": "https://example.net", "content": "x"},
            ]
        }
    )
    tools = WebTools("private-test-key", transport=transport)

    result = await tools.web_search("  current news  ", max_results=2)

    request = seen[0]
    assert str(request.url) == "https://ollama.com/api/web_search"
    assert request.headers["Authorization"] == "Bearer private-test-key"
    assert json.loads(request.content) == {
        "query": "current news",
        "max_results": 2,
    }
    assert len(result["results"]) == 2
    assert len(result["results"][0]["title"]) == 512
    assert len(result["results"][0]["url"]) == 2048
    assert sum(len(item["content"]) for item in result["results"]) <= 20_000
    assert result["truncated"] is True


async def test_search_no_results_is_not_marked_truncated():
    transport, _ = _transport({"results": []})
    result = await WebTools("key", transport=transport).web_search("query")
    assert result == {"query": "query", "results": [], "truncated": False}


async def test_fetch_posts_url_as_json_to_fixed_endpoint_and_bounds_output():
    requested_url = "https://example.com/article"
    transport, seen = _transport(
        {
            "title": "T" * 600,
            "content": "C" * 21_000,
            "links": ["https://example.com/" + "l" * 2100] * 21,
        }
    )

    result = await WebTools("key", transport=transport).web_fetch(requested_url)

    assert str(seen[0].url) == "https://ollama.com/api/web_fetch"
    assert json.loads(seen[0].content) == {"url": requested_url}
    assert result["url"] == requested_url
    assert len(result["title"]) == 512
    assert len(result["content"]) == 20_000
    assert len(result["links"]) == 20
    assert all(len(link) == 2048 for link in result["links"])
    assert result["truncated"] is True


@pytest.mark.parametrize(
    "url",
    [
        "",
        "ftp://example.com/file",
        "https:///missing-host",
        "https://user:password@example.com/private",
        "https://[invalid",
    ],
)
async def test_invalid_fetch_url_returns_error_without_request(url):
    transport, seen = _transport({})
    result = await WebTools("key", transport=transport).web_fetch(url)
    assert "error" in result
    assert seen == []


@pytest.mark.parametrize("query", ["", " \t\n"])
async def test_blank_search_query_returns_error_without_request(query):
    transport, seen = _transport({})
    result = await WebTools("key", transport=transport).web_search(query)
    assert "error" in result
    assert seen == []


@pytest.mark.parametrize("max_results", [0, 11, -1, True, 2.5])
async def test_invalid_result_count_returns_error_without_request(max_results):
    transport, seen = _transport({})
    result = await WebTools("key", transport=transport).web_search(
        "query", max_results=max_results
    )
    assert "error" in result
    assert seen == []


@pytest.mark.parametrize(
    ("status", "expected"),
    [
        (401, "rejected web access"),
        (403, "rejected web access"),
        (429, "rate limited"),
        (500, "request failed"),
    ],
)
async def test_http_errors_are_safe_and_do_not_include_response_body(status, expected):
    transport, _ = _transport({"secret": "response body"}, status=status)
    result = await WebTools("private-key", transport=transport).web_search("query")
    assert expected in result["error"]
    assert "private-key" not in result["error"]
    assert "response body" not in result["error"]


@pytest.mark.parametrize("body", ["not-json", {"results": [{"title": 1}]}])
async def test_invalid_response_json_or_schema_is_reported_safely(body):
    if isinstance(body, str):
        transport = httpx.MockTransport(
            lambda request: httpx.Response(200, content=body.encode())
        )
    else:
        transport, _ = _transport(body)
    result = await WebTools("key", transport=transport).web_search("query")
    assert result == {"error": "Ollama returned an invalid web response."}


async def test_timeout_and_network_errors_return_safe_errors():
    def timeout_handler(request):
        raise httpx.ReadTimeout("sensitive request details", request=request)

    timeout_transport = httpx.MockTransport(timeout_handler)
    timeout_result = await WebTools(
        "secret-key", transport=timeout_transport
    ).web_fetch("https://example.com")
    assert timeout_result == {"error": "The Ollama web request timed out."}

    def network_handler(request):
        raise httpx.ConnectError("sensitive request details", request=request)

    network_transport = httpx.MockTransport(network_handler)
    network_result = await WebTools(
        "secret-key", transport=network_transport
    ).web_search("private query")
    assert network_result == {"error": "The Ollama web service could not be reached."}
    assert "secret-key" not in str(network_result)
    assert "private query" not in str(network_result)


async def test_cancellation_propagates():
    async def cancel_handler(request):
        raise asyncio.CancelledError

    transport = httpx.MockTransport(cancel_handler)
    with pytest.raises(asyncio.CancelledError):
        await WebTools("key", transport=transport).web_search("query")


async def test_total_request_deadline(monkeypatch):
    from ai_chat import web

    monkeypatch.setattr(web, "_REQUEST_TIMEOUT", 0.01)

    async def slow_handler(request):
        await asyncio.sleep(1)
        return httpx.Response(200, json={"results": []})

    result = await WebTools(
        "key", transport=httpx.MockTransport(slow_handler)
    ).web_search("query")
    assert result == {"error": "The Ollama web request timed out."}


async def test_fetch_rejects_oversized_url_before_request():
    transport, seen = _transport({})
    result = await WebTools("key", transport=transport).web_fetch(
        "https://example.com/" + "x" * 2048
    )
    assert "2048" in result["error"]
    assert not seen
