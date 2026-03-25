FROM python:3.11-slim

ENV PYTHONDONTWRITEBYTECODE=1 \
    PYTHONUNBUFFERED=1 \
    PIP_NO_CACHE_DIR=1

WORKDIR /app

COPY sdk/python/requirements.txt /tmp/requirements.txt
RUN pip install -r /tmp/requirements.txt

COPY sdk/python /app/sdk/python
RUN pip install /app/sdk/python

COPY fixtures/pytorch_ddp_counter /app/fixtures/pytorch_ddp_counter

WORKDIR /app/fixtures/pytorch_ddp_counter
ENTRYPOINT ["python", "train.py"]
