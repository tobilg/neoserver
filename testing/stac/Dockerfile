FROM python:3.12-slim
WORKDIR /app
COPY testing/stac/requirements.lock /app/testing/stac/requirements.lock
RUN pip install --no-cache-dir --require-hashes -r /app/testing/stac/requirements.lock
COPY testing/stac/run.py /app/testing/stac/run.py
COPY testing/stac/schemas /app/testing/stac/schemas
COPY internal/stacmodel/schemas /app/internal/stacmodel/schemas
ENTRYPOINT ["python", "/app/testing/stac/run.py"]
