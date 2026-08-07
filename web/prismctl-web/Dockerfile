# syntax=docker/dockerfile:1.7

FROM node:22.14.0-alpine AS deps
WORKDIR /app
COPY package.json package-lock.json ./
RUN npm ci

FROM deps AS dev
COPY . .
EXPOSE 5173
CMD ["npm", "run", "dev"]

FROM deps AS test
COPY . .
CMD ["sh", "-c", "npm run build && npm test"]

FROM deps AS build
COPY . .
RUN npm run build

FROM mcr.microsoft.com/playwright:v1.51.1-noble AS e2e
WORKDIR /app
COPY --from=deps /app/node_modules ./node_modules
COPY . .
CMD ["npm", "run", "test:e2e"]

FROM nginxinc/nginx-unprivileged:1.27.4-alpine AS runtime
COPY --from=build /app/dist /usr/share/nginx/html
COPY nginx/default.conf /etc/nginx/conf.d/default.conf
EXPOSE 8080
