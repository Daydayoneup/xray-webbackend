# 1) 取 xray 二进制 + geodata
FROM ghcr.io/xtls/xray-core:latest AS xray

# 2) 构建前端 → dist
FROM node:20-slim AS web
WORKDIR /web
COPY frontend/package.json frontend/package-lock.json ./
RUN npm ci
COPY frontend/ ./
RUN npm run build

# 3) python 运行时
FROM python:3.12-slim
COPY --from=xray /usr/local/bin/xray /usr/local/bin/xray
COPY --from=xray /usr/local/share/xray/ /usr/local/share/xray/
ENV XRAY_LOCATION_ASSET=/usr/local/share/xray
WORKDIR /app
RUN pip install --no-cache-dir uv
COPY pyproject.toml uv.lock ./
RUN uv sync --frozen --no-dev
COPY backend/ ./backend/
COPY --from=web /web/dist ./frontend_dist/
EXPOSE 2017 10808 10809
CMD uv run uvicorn --factory backend.main:create_app --host ${PANEL_LISTEN:-0.0.0.0} --port ${PANEL_PORT:-2017}
