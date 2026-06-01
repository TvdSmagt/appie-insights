FROM python:3.12-slim

WORKDIR /app

COPY dashboard/requirements.txt .
RUN pip install --no-cache-dir -r requirements.txt

COPY dashboard/ .

RUN useradd -u 1000 -m appuser \
    && mkdir -p /app/config \
    && chown appuser:appuser /app/config \
    && chmod +x /app/entrypoint.sh
USER appuser

EXPOSE 8501
ENTRYPOINT ["/app/entrypoint.sh"]
