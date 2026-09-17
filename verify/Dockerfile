FROM node:20-alpine
WORKDIR /verify
COPY verify.js .
ENV API_URL=http://api:8080 \
    WEB_URL=http://web
CMD ["node", "verify.js"]
