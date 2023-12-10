#FROM node:18.18.2-alpine3.18 as build
FROM node:16.20-alpine3.18 as build
WORKDIR /opt
RUN apk add --no-cache python3 make g++
COPY . .
RUN cd web && yarn add -g verb-cli && yarn install && yarn build
FROM cdfng/caddy:2.7.5 as web
COPY --from=build /opt/web/dist /srv
COPY --from=build /opt/web/Caddyfile /etc/caddy/Caddyfile
