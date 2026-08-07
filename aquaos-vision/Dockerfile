FROM python:3.12-slim
RUN useradd --system --uid 10001 vision
WORKDIR /app
COPY pyproject.toml ./
COPY src ./src
RUN pip install --no-cache-dir .
USER vision
CMD ["python", "-m", "aquaos_vision"]
