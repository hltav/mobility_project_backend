# Backend Dockerfile for Mobility Project
# Builds the Laravel API and serves it with php artisan serve.

FROM node:20-alpine AS node-builder
WORKDIR /var/www/html
COPY package*.json vite.config.js ./
COPY resources resources
RUN npm install
COPY . .
RUN npm run build

FROM php:8.3-fpm
RUN apt-get update && apt-get install -y \
    git \
    curl \
    zip \
    unzip \
    libzip-dev \
    libonig-dev \
    libxml2-dev \
    libicu-dev \
    && docker-php-ext-install pdo_mysql mbstring exif pcntl bcmath zip intl \
    && rm -rf /var/lib/apt/lists/*

COPY --from=composer:latest /usr/bin/composer /usr/bin/composer
WORKDIR /var/www/html
COPY composer.json composer.lock ./
RUN composer install --prefer-dist --no-progress --no-interaction
COPY . .
COPY --from=node-builder /var/www/html/public/build ./public/build
RUN composer dump-autoload --optimize

EXPOSE 8000
CMD ["sh", "-c", "if [ ! -f .env ]; then cp .env.example .env; fi && php artisan key:generate --force && php artisan serve --host=0.0.0.0 --port=8000"]