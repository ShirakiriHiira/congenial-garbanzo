# syntax=docker/dockerfile:1
FROM node:24.22.0-alpine3.23 AS build
WORKDIR /app
COPY package.json package-lock.json ./
RUN npm ci
COPY . .
RUN npm run check && npm run test && npm run build

FROM nginxinc/nginx-unprivileged:1.28.0-alpine
COPY --from=build /app/dist/ /usr/share/nginx/html/app/
COPY --from=build /app/dist/nginx.conf /etc/nginx/conf.d/default.conf
HEALTHCHECK --interval=30s --timeout=3s --start-period=10s --retries=3 CMD wget -q -O /dev/null http://127.0.0.1:8080/app/index.html || exit 1
