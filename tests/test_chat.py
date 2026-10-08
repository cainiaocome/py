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
