
# Backend Dockerfile for Mobility Project
# Builds the Laravel API and serves it with php artisan serve.

# Isolated CLI target: no web server, importers or automatic migrations.
FROM php:8.3-cli AS postgres-tools
RUN apt-get update && apt-get install -y --no-install-recommends libpq-dev libonig-dev libxml2-dev \
    && docker-php-ext-install pdo_pgsql mbstring dom xml xmlwriter \
    && rm -rf /var/lib/apt/lists/*
WORKDIR /workspace

FROM node:20-alpine AS node-builder

WORKDIR /var/www/html

# Atualiza o npm dentro da imagem de build
RUN npm install -g npm@10.9.4

COPY package*.json vite.config.js ./
COPY resources resources

# Instala as dependências do frontend Laravel
# Requer package-lock.json válido
RUN npm ci --no-audit --no-fund

# .dockerignore excludes runtime secrets from every stage, including this builder.
COPY . .

RUN npm run build

FROM php:8.3-fpm

# Install PhpRedis in the final PHP runtime, preserving the existing extensions.
RUN apt-get update && apt-get install -y \
    git \
    curl \
    zip \
    unzip \
    libzip-dev \
    libonig-dev \
    libxml2-dev \
    libicu-dev \
    libpq-dev \
    && docker-php-ext-install pdo_mysql pdo_pgsql mbstring exif pcntl bcmath zip intl \
    && pecl install redis-6.1.0 \
    && docker-php-ext-enable redis \
    && rm -rf /var/lib/apt/lists/*

COPY --from=composer:latest /usr/bin/composer /usr/bin/composer

WORKDIR /var/www/html

COPY composer.json composer.lock ./

RUN composer install \
    --prefer-dist \
    --no-progress \
    --no-interaction \
    --no-scripts

COPY entrypoint.sh /entrypoint.sh
RUN chmod +x /entrypoint.sh

COPY . .

# Local runtime data/cache are excluded from the context; retain Laravel's dirs.
RUN mkdir -p bootstrap/cache storage/app/private storage/app/public \
    storage/framework/cache/data storage/framework/sessions \
    storage/framework/views storage/logs

COPY --from=node-builder /var/www/html/public/build ./public/build

RUN composer dump-autoload --optimize

EXPOSE 8000

ENTRYPOINT ["/entrypoint.sh"]
