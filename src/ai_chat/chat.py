"""LLM configuration and session-local conversation history."""

from collections.abc import AsyncIterator

from pydantic_ai import Agent
from pydantic_ai.messages import ModelMessage
from pydantic_ai.models.openai import OpenAIChatModel
from pydantic_ai.providers.openai import OpenAIProvider


class Chat:
    def __init__(self, agent: Agent, provider: OpenAIProvider | None = None) -> None:
        self.agent = agent
        self.provider = provider
        self.history: list[ModelMessage] = []

    @property
    def model_name(self) -> str:
        return self.agent.model.model_name

    async def list_models(self) -> list[str]:
        if self.provider is None:
            raise ValueError("Model discovery requires an Ollama Cloud provider")
        models = await self.provider.client.models.list()
        return sorted({model.id for model in models.data})

    def switch_model(self, model: str) -> None:
        if self.provider is None:
            raise ValueError("Model switching requires an Ollama Cloud provider")
        model = model.strip()
        if not model or any(char.isspace() for char in model):
            raise ValueError("Enter a model name without whitespace")
        self.agent = Agent(OpenAIChatModel(model, provider=self.provider))

    def clear(self) -> None:
        self.history.clear()

    async def stream(self, prompt: str) -> AsyncIterator[str]:
        """Yield cumulative text; retain history only after successful completion."""
        async with self.agent.run_stream(
            prompt, message_history=self.history
        ) as result:
            async for text in result.stream_text(delta=False):
                yield text
            messages = result.all_messages()
        self.history = messages


def create_chat(api_key: str, model: str) -> Chat:
    provider = OpenAIProvider(base_url="https://ollama.com/v1", api_key=api_key)
    return Chat(Agent(OpenAIChatModel(model, provider=provider)), provider)
