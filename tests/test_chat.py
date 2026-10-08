import pytest
from pydantic_ai import Agent
from pydantic_ai.messages import ModelRequest, ModelResponse, TextPart, UserPromptPart
from pydantic_ai.models.function import FunctionModel

from ai_chat.chat import Chat


async def test_stream_retains_context_and_clear_resets_it():
    requests = []

    async def stream(messages, info):
        requests.append(list(messages))
        yield "Hello"
        yield " world"

    chat = Chat(Agent(FunctionModel(stream_function=stream)))
    chunks = [text async for text in chat.stream("first")]
    assert chunks[-1] == "Hello world"
    assert len(chat.history) == 2
    assert isinstance(chat.history[-1], ModelResponse)
    assert any(
        isinstance(p, TextPart) and p.content == "Hello world"
        for p in chat.history[-1].parts
    )
    _ = [text async for text in chat.stream("second")]
    assert len(requests[1]) == 3
    assert isinstance(requests[1][0], ModelRequest)
    assert isinstance(requests[1][0].parts[0], UserPromptPart)
    assert requests[1][0].parts[0].content == "first"
    assert len(chat.history) == 4
    chat.clear()
    _ = [text async for text in chat.stream("fresh")]
    assert len(requests[2]) == 1


async def test_failed_stream_preserves_previous_history():
    count = 0

    async def stream(messages, info):
        nonlocal count
        count += 1
        yield "partial"
        if count == 2:
            raise RuntimeError("network failure")

    chat = Chat(Agent(FunctionModel(stream_function=stream)))
    _ = [text async for text in chat.stream("first")]
    previous = list(chat.history)
    with pytest.raises(RuntimeError):
        _ = [text async for text in chat.stream("second")]
    assert chat.history == previous


def test_model_switch_preserves_context_and_provider():
    from ai_chat.chat import create_chat

    chat = create_chat("test-key", "old-model")
    history = [ModelRequest(parts=[UserPromptPart(content="remember")])]
    chat.history = history
    provider = chat.provider
    chat.switch_model("new-model")
    assert chat.model_name == "new-model"
    assert chat.history is history
    assert chat.agent.model.provider is provider
    with pytest.raises(ValueError):
        chat.switch_model("bad model")
    assert chat.model_name == "new-model"


async def test_mixed_text_search_fetch_and_followup_history():
    import httpx
    from pydantic_ai.messages import ToolCallPart, ToolReturnPart
    from pydantic_ai.models.function import DeltaToolCall

    from ai_chat.web import WebTools

    calls = []
    requests = []

    def respond(request):
        calls.append(request.url.path)
        if request.url.path.endswith("web_search"):
            return httpx.Response(
                200,
                json={
                    "results": [
                        {
                            "title": "Example",
                            "url": "https://example.com/",
                            "content": "Snippet",
                        }
                    ]
                },
            )
        return httpx.Response(
            200, json={"title": "Example", "content": "Page content", "links": []}
        )

    web = WebTools("test-key", transport=httpx.MockTransport(respond))

    async def stream(messages, info):
        requests.append(list(messages))
        assert {tool.name for tool in info.function_tools} == {
            "web_search",
            "web_fetch",
        }
        if len(requests) == 1:
            yield "Let me check."
            yield {
                0: DeltaToolCall(
                    name="web_search",
                    json_args='{"query":"example"}',
                    tool_call_id="search-1",
                )
            }
        elif len(requests) == 2:
            yield {
                0: DeltaToolCall(
                    name="web_fetch",
                    json_args='{"url":"https://example.com/"}',
                    tool_call_id="fetch-1",
                )
            }
        else:
            yield "Answer with [source](https://example.com/)."

    chat = Chat(
        Agent(
            FunctionModel(stream_function=stream), tools=[web.web_search, web.web_fetch]
        )
    )
    activity = []
    chunks = [
        text
        async for text in chat.stream("Research example", on_activity=activity.append)
    ]
    assert chunks[-1] == "Let me check.\n\nAnswer with [source](https://example.com/)."
    assert calls == ["/api/web_search", "/api/web_fetch"]
    assert activity == ["Searching the web…", "Fetching page…"]
    assert len(chat.history) == 6
    parts = [part for message in chat.history for part in message.parts]
    assert sum(isinstance(part, ToolCallPart) for part in parts) == 2
    assert sum(isinstance(part, ToolReturnPart) for part in parts) == 2
    _ = [text async for text in chat.stream("What did you find?")]
    assert len(requests[-1]) == 7
    chat.clear()
    assert chat.history == []


async def test_research_can_exceed_previous_and_framework_default_limits():
    from pydantic_ai.models.function import DeltaToolCall

    calls = []
    requests = 0

    async def web_search(query: str) -> str:
        calls.append(query)
        return "Found a result"

    async def stream(messages, info):
        nonlocal requests
        requests += 1
        if len(calls) < 55:
            yield {
                0: DeltaToolCall(
                    name="web_search",
                    json_args='{"query":"next source"}',
                    tool_call_id=f"call-{len(calls)}",
                )
            }
        else:
            yield "Research complete."

    chat = Chat(Agent(FunctionModel(stream_function=stream), tools=[web_search]))
    chunks = [text async for text in chat.stream("Research many sources")]
    assert chunks[-1] == "Research complete."
    assert len(calls) == 55
    assert requests == 56
    assert len(chat.history) == 112


async def test_cancelled_tool_preserves_history():
    import asyncio

    from pydantic_ai.models.function import DeltaToolCall

    started = asyncio.Event()

    async def web_fetch(url: str) -> str:
        started.set()
        await asyncio.Event().wait()
        return "unreachable"

    async def stream(messages, info):
        yield {
            0: DeltaToolCall(
                name="web_fetch",
                json_args='{"url":"https://example.com/"}',
                tool_call_id="fetch-1",
            )
        }

    chat = Chat(Agent(FunctionModel(stream_function=stream), tools=[web_fetch]))
    previous = [
        ModelRequest(parts=[UserPromptPart(content="previous")]),
        ModelResponse(parts=[TextPart(content="answer")]),
    ]
    chat.history = list(previous)

    async def consume():
        return [text async for text in chat.stream("Fetch")]

    task = asyncio.create_task(consume())
    await asyncio.wait_for(started.wait(), timeout=5)
    task.cancel()
    with pytest.raises(asyncio.CancelledError):
        await task
    assert chat.history == previous


async def test_model_switch_keeps_web_tools():
    from ai_chat.chat import create_chat

    chat = create_chat("test-key", "old-model")
    web = chat.web
    chat.enable_web()
    chat.switch_model("new-model")
    assert chat.web is web

    async def stream(messages, info):
        assert {tool.name for tool in info.function_tools} == {
            "web_search",
            "web_fetch",
        }
        yield "Web tools are available."

    with chat.agent.override(model=FunctionModel(stream_function=stream)):
        chunks = [text async for text in chat.stream("Check available tools")]
    assert chunks[-1] == "Web tools are available."


async def test_web_error_is_available_to_model_for_recovery():
    import httpx
    from pydantic_ai.messages import ToolReturnPart
    from pydantic_ai.models.function import DeltaToolCall

    from ai_chat.web import WebTools

    web = WebTools(
        "secret-key", transport=httpx.MockTransport(lambda request: httpx.Response(429))
    )
    count = 0

    async def stream(messages, info):
        nonlocal count
        count += 1
        if count == 1:
            yield {
                0: DeltaToolCall(
                    name="web_search",
                    json_args='{"query":"example"}',
                    tool_call_id="search-1",
                )
            }
        else:
            returned = [
                p for m in messages for p in m.parts if isinstance(p, ToolReturnPart)
            ]
            assert "rate limited" in returned[-1].content["error"]
            yield "Web search is rate limited. Please try later."

    chat = Chat(Agent(FunctionModel(stream_function=stream), tools=[web.web_search]))
    chunks = [text async for text in chat.stream("Search")]
    assert "rate limited" in chunks[-1]
    assert len(chat.history) == 4


async def test_web_tools_are_opt_in_and_enable_preserves_context():
    from ai_chat.chat import create_chat

    chat = create_chat("test-key", "test-model")
    chat.switch_model("second-model")

    async def offline(messages, info):
        assert info.function_tools == []
        yield "Offline reply."

    with chat.agent.override(model=FunctionModel(stream_function=offline)):
        _ = [text async for text in chat.stream("hello")]
    previous = list(chat.history)
    assert not chat.web_enabled
    assert chat.enable_web() is True
    assert chat.history == previous
    assert chat.enable_web() is False
    assert chat.web_enabled
    chat.clear()
    assert chat.web_enabled

    async def enabled(messages, info):
        assert {tool.name for tool in info.function_tools} == {
            "web_search",
            "web_fetch",
        }
        yield "Web enabled."

    with chat.agent.override(model=FunctionModel(stream_function=enabled)):
        _ = [text async for text in chat.stream("hello again")]
    fresh = create_chat("test-key", "test-model")
    assert not fresh.web_enabled
