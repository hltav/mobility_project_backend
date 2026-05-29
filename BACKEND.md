# 🔴 Backend - Documentação Completa

> **Urban Mobility API** — API RESTful robusta construída com Laravel 11 para gerenciar usuários, autenticação e acesso a dados de transporte público de São Paulo em tempo real.

---

## 📋 Índice

1. [Visão Geral](#-visão-geral)
2. [Stack Tecnológico](#-stack-tecnológico)
3. [Arquitetura](#-arquitetura)
4. [Estrutura de Diretórios](#-estrutura-de-diretórios)
5. [Setup e Instalação](#-setup-e-instalação)
6. [Variáveis de Ambiente](#-variáveis-de-ambiente)
7. [Autenticação](#-autenticação)
8. [Models e Relacionamentos](#-models-e-relacionamentos)
9. [Serviços e Lógica de Negócio](#-serviços-e-lógica-de-negócio)
10. [Endpoints Principais](#-endpoints-principais)
11. [Tratamento de Erros](#-tratamento-de-erros)
12. [Testes](#-testes)
13. [Decisões de Design](#-decisões-de-design)

---

## 🎯 Visão Geral

O backend do Mobility Project é uma API RESTful que centraliza toda a lógica de negócio:

- **Autenticação** de usuários via tokens (Sanctum) e OAuth (Google)
- **Gestão de usuários** com integração OAuth
- **Catálogo de linhas** com busca e filtros
- **Posicionamento em tempo real** de ônibus (SPTrans OlhoVivo)
- **Sistema de favoritos** por usuário
- **Geolocalização** de linhas e paradas
- **Caching inteligente** de dados imutáveis

---

## 🚀 Stack Tecnológico

| Componente | Versão | Descrição |
|-----------|--------|-----------|
| **PHP** | 8.3+ | Linguagem principal |
| **Laravel** | 11 | Framework web |
| **MySQL** | 8+ | Banco de dados principal |
| **Redis** | Latest | Cache distribuído (opcional) |
| **Laravel Sanctum** | 4.3+ | Autenticação API |
| **Laravel Socialite** | 5.27+ | OAuth (Google) |
| **Guzzle** | Via Laravel Http | Cliente HTTP para APIs externas |
| **Vite** | 8.0+ | Build tool frontend |
| **Tailwind CSS** | 4.0+ | Utility-first CSS |
| **PHPUnit** | 12.5+ | Framework de testes |
| **Faker** | 1.23+ | Geração de dados fake |

---

## 🧠 Arquitetura

### Modelo de Camadas

O backend segue a arquitetura **Service Layer** com separação clara de responsabilidades:

```
┌─────────────────────────────────────────────────────────┐
│                   HTTP REQUEST                          │
│              (Router: routes/api.php)                   │
└────────────────────────┬────────────────────────────────┘
                         │
                         ▼
┌─────────────────────────────────────────────────────────┐
│                 MIDDLEWARE CHAIN                        │
│    (Auth, CORS, Rate Limiting, Validation)             │
└────────────────────────┬────────────────────────────────┘
                         │
                         ▼
┌─────────────────────────────────────────────────────────┐
│              FORM REQUESTS                              │
│          (Validação de entrada)                         │
│   LoginRequest / RegisterRequest / SearchLineRequest   │
└────────────────────────┬────────────────────────────────┘
                         │
                         ▼
┌─────────────────────────────────────────────────────────┐
│                 CONTROLLERS                             │
│   AuthController / LineController / BusController      │
│   FavoriteController / BusStopController               │
└────────────────────────┬────────────────────────────────┘
                         │
                         ▼
┌─────────────────────────────────────────────────────────┐
│                   SERVICES                              │
│   ┌──────────────────────────────────────────────────┐  │
│   │ AuthService      - Autenticação e registro       │  │
│   │ LineService      - Lógica de linhas              │  │
│   │ BusStopService   - Lógica de paradas             │  │
│   │ ExternalApiService - Integração SPTrans          │  │
│   │ ProcessorService - Integração com Go Processor   │  │
│   └──────────────────────────────────────────────────┘  │
└────────────────┬───────────────────────────────────────┘
                 │
                 ▼
┌─────────────────────────────────────────────────────────┐
│               REPOSITORIES                              │
│   LineRepository / FavoriteRepository / BusRepository  │
└────────────────────────┬────────────────────────────────┘
                         │ (Eloquent ORM)
                         ▼
┌─────────────────────────────────────────────────────────┐
│              ELOQUENT MODELS                            │
│   User / Line / Bus / BusStop / Favorite               │
└────────────────────────┬────────────────────────────────┘
                         │
                         ▼
┌─────────────────────────────────────────────────────────┐
│            DATABASE (MySQL)                             │
└─────────────────────────────────────────────────────────┘
```

### Fluxo de uma Requisição

```
1. Cliente envia POST /api/lines/search?term=100
   ↓
2. Router match (routes/api.php)
   ↓
3. Middleware 'auth:sanctum' valida token
   ↓
4. LineController@search é invocado
   ↓
5. LineService->search() executa lógica
   ↓
6. LineRepository->search() query BD
   ↓
7. Models são retornados como Collection
   ↓
8. DTOs convertem Models para JSON
   ↓
9. Response é serializado e retornado
```

---

## 📁 Estrutura de Diretórios

```
mobility_backend/
├── app/
│   ├── Console/
│   │   └── Commands/              # Comandos Artisan customizados
│   │
│   ├── DTOs/
│   │   ├── BusDTO.php             # Data Transfer Object para Bus
│   │   ├── LineDTO.php            # Data Transfer Object para Line
│   │   └── ...
│   │
│   ├── Http/
│   │   ├── Controllers/
│   │   │   ├── Controller.php      # Base controller
│   │   │   ├── AuthController.php  # Autenticação
│   │   │   ├── LineController.php  # Linhas
│   │   │   ├── BusController.php   # Ônibus
│   │   │   ├── BusStopController.php  # Paradas
│   │   │   ├── FavoriteController.php # Favoritos
│   │   │   └── GeocodingController.php # Geocodificação
│   │   │
│   │   └── Requests/
│   │       ├── LoginRequest.php    # Validação login
│   │       ├── RegisterRequest.php # Validação registro
│   │       ├── SearchLineRequest.php
│   │       └── ...
│   │
│   ├── Models/
│   │   ├── User.php               # Usuário com OAuth
│   │   ├── Line.php               # Linha de ônibus
│   │   ├── Bus.php                # Ônibus (relação com Line)
│   │   ├── BusStop.php            # Parada de ônibus
│   │   ├── Favorite.php           # Linhas favoritas
│   │   └── ...
│   │
│   ├── Repositories/
│   │   ├── LineRepository.php      # Acesso a linhas
│   │   ├── FavoriteRepository.php  # Acesso a favoritos
│   │   └── ...
│   │
│   ├── Services/
│   │   ├── AuthService.php         # Lógica de autenticação
│   │   ├── LineService.php         # Lógica de linhas
│   │   ├── BusStopService.php      # Lógica de paradas
│   │   ├── ExternalApiService.php  # Integração SPTrans
│   │   ├── ProcessorService.php    # Integração Go Processor
│   │   └── ...
│   │
│   └── Providers/
│       └── AppServiceProvider.php  # Service Provider principal
│
├── bootstrap/
│   ├── app.php                     # Bootstrap da aplicação
│   └── providers.php               # Providers registrados
│
├── config/
│   ├── app.php                     # Config geral
│   ├── auth.php                    # Config autenticação
│   ├── cache.php                   # Config cache
│   ├── database.php                # Config banco de dados
│   ├── filesystems.php             # Config storage
│   ├── logging.php                 # Config logging
│   ├── mail.php                    # Config email
│   ├── queue.php                   # Config filas
│   ├── sanctum.php                 # Config Sanctum
│   ├── services.php                # Config serviços externos
│   └── session.php                 # Config sessões
│
├── database/
│   ├── migrations/
│   │   ├── 2024_create_users_table.php
│   │   ├── 2024_create_lines_table.php
│   │   ├── 2024_create_buses_table.php
│   │   ├── 2024_create_bus_stops_table.php
│   │   ├── 2024_create_favorites_table.php
│   │   └── ...
│   │
│   ├── seeders/
│   │   ├── DatabaseSeeder.php
│   │   └── ...
│   │
│   └── factories/
│       ├── UserFactory.php
│       ├── LineFactory.php
│       └── ...
│
├── public/
│   └── index.php                   # Entry point
│
├── resources/
│   ├── css/
│   │   └── app.css                 # CSS principal (Tailwind)
│   │
│   ├── js/
│   │   └── app.js                  # JS principal
│   │
│   └── views/
│       └── ...                     # Views (se houver UI)
│
├── routes/
│   ├── api.php                     # Rotas da API
│   ├── web.php                     # Rotas web (se houver)
│   └── console.php                 # Rotas console
│
├── storage/
│   ├── app/                        # Arquivos da aplicação
│   ├── framework/                  # Arquivos do framework
│   └── logs/                       # Arquivos de log
│
├── tests/
│   ├── TestCase.php                # Base para testes
│   ├── Feature/
│   │   ├── AuthTest.php
│   │   ├── LineTest.php
│   │   └── ...
│   │
│   └── Unit/
│       ├── AuthServiceTest.php
│       ├── LineServiceTest.php
│       └── ...
│
├── vendor/                         # Dependências Composer
│
├── .env                            # Variáveis de ambiente
├── .env.example                    # Exemplo de .env
├── .gitignore                      # Git ignore
├── artisan                         # CLI do Laravel
├── composer.json                   # Dependências PHP
├── composer.lock                   # Lock de dependências
├── package.json                    # Dependências Node
├── package-lock.json               # Lock de dependências Node
├── phpunit.xml                     # Config PHPUnit
├── vite.config.js                  # Config Vite
├── tailwind.config.js              # Config Tailwind
├── README.md                       # Documentação backend
└── ...
```

---

## 🔧 Setup e Instalação

### Pré-requisitos

```bash
# Verificar versões
php -v              # PHP 8.3+
composer --version  # Composer 2.0+
node --version      # Node 18+
mysql --version     # MySQL 8+
```

### Instalação Completa

```bash
# 1. Entrar no diretório backend
cd mobility_backend

# 2. Instalar dependências PHP
composer install

# 3. Criar arquivo .env
cp .env.example .env

# 4. Gerar chave da aplicação
php artisan key:generate

# 5. Executar migrações
php artisan migrate

# 6. Instalar dependências Node
npm install

# 7. Build assets
npm run build

# 8. Iniciar servidor de desenvolvimento
php artisan serve
# Acessível em http://localhost:8000
```

### Desenvolvimento com Watch

```bash
# Terminal 1: Servidor Laravel
php artisan serve

# Terminal 2: Queue worker (processa jobs)
php artisan queue:listen --tries=1 --timeout=0

# Terminal 3: Logs em tempo real
php artisan pail

# Terminal 4: Vite (rebuilda assets)
npm run dev

# OU tudo junto:
npm run dev  # Usa concurrently
```

---

## 🔐 Variáveis de Ambiente

### Variáveis Essenciais

```env
# ===== APP CONFIG =====
APP_NAME=mobility_project
APP_ENV=local                    # local, production
APP_DEBUG=true                   # true em dev, false em prod
APP_KEY=base64:...              # Gerado com php artisan key:generate
APP_URL=http://localhost:8000

# ===== DATABASE =====
DB_CONNECTION=mysql
DB_HOST=127.0.0.1
DB_PORT=3306
DB_DATABASE=mobility_project
DB_USERNAME=mobility_user
DB_PASSWORD=mobility_2026

# ===== CACHE =====
CACHE_STORE=database             # database, redis, file
CACHE_PREFIX=

# ===== SESSION =====
SESSION_DRIVER=database
SESSION_LIFETIME=120             # minutos
SESSION_ENCRYPT=false

# ===== QUEUE =====
QUEUE_CONNECTION=database        # database, redis, sync

# ===== SANCTUM (API TOKENS) =====
# Nenhuma variável necessária - usa BD

# ===== AUTENTICAÇÃO SOCIAL =====
GOOGLE_CLIENT_ID=your_client_id
GOOGLE_CLIENT_SECRET=your_secret
GOOGLE_REDIRECT_URI=http://localhost:8000/api/auth/google/callback

# ===== EXTERNAL API =====
SPTRANS_API_TOKEN=your_sptrans_token
GOOGLE_MAPS_KEY=your_google_maps_key

# ===== URLS =====
LARAVEL_URL=http://localhost:8000
PROCESSOR_URL=http://localhost:3000    # URL do Go Processor

# ===== MAIL (opcional) =====
MAIL_MAILER=log
MAIL_FROM_ADDRESS="hello@example.com"

# ===== LOGGING =====
LOG_CHANNEL=stack
LOG_LEVEL=debug                 # debug, info, notice, warning, error, critical, alert, emergency
```

### Variáveis Recomendadas em Produção

```env
APP_DEBUG=false
APP_ENV=production
CACHE_STORE=redis
SESSION_DRIVER=database
QUEUE_CONNECTION=redis
LOG_CHANNEL=single
```

---

## 🔐 Autenticação

### Estratégias Suportadas

#### 1. **Token-Based (Sanctum)**

```php
// Fluxo de login
POST /api/auth/login
{
    "email": "user@example.com",
    "password": "password123"
}

// Response
{
    "token": "1|abc123xyz...",
    "user": {
        "id": 1,
        "name": "João",
        "email": "user@example.com"
    }
}

// Usar token
Authorization: Bearer 1|abc123xyz...
```

**Vantagens:**

- Stateless
- Seguro contra CSRF
- Funciona com SPAs e apps mobile
- Suporte multi-device (múltiplos tokens por user)

#### 2. **OAuth Google (Socialite)**

```php
// Redirect para Google
GET /api/auth/google

// Google redireciona para
GET /api/auth/google/callback?code=...

// Response
{
    "token": "1|abc123xyz...",
    "user": {
        "id": 1,
        "name": "João Silva",
        "email": "joao@gmail.com",
        "avatar": "https://..."
    }
}
```

**Fluxo:**

1. Cliente redireciona usuário para `/api/auth/google`
2. Google autentica usuário
3. Google redireciona para `/api/auth/google/callback`
4. Backend cria/atualiza usuário no BD
5. Token é retornado

### Middleware Proteção

```php
// routes/api.php
Route::middleware('auth:sanctum')->group(function () {
    // Rotas protegidas aqui
    Route::get('/lines', [LineController::class, 'index']);
});
```

### Logout

```php
POST /api/auth/logout
Authorization: Bearer token

// Response
{
    "message": "Logout realizado com sucesso"
}
```

---

## 📊 Models e Relacionamentos

### User

```php
// Atributos
id          : integer (PK)
name        : string
email       : string (unique)
password    : string (hashed)
google_id   : string (nullable, unique)
avatar      : string (nullable, URL)
created_at  : timestamp
updated_at  : timestamp

// Relacionamentos
favorites() : HasMany       // Linhas favoritas do usuário
tokens()    : HasMany       // Tokens Sanctum
```

### Line

```php
// Atributos
id              : integer (PK)
sptrans_id      : integer (unique)
direction_1     : string     // "Centro" ou similar
direction_2     : string     // "Zona Sul" ou similar
main_stop_1_id  : integer (FK)
main_stop_2_id  : integer (FK)
lat_1           : decimal (nullable)
lng_1           : decimal (nullable)
lat_2           : decimal (nullable)
lng_2           : decimal (nullable)
color           : string (hex)
created_at      : timestamp
updated_at      : timestamp

// Relacionamentos
buses()     : HasMany           // Ônibus nesta linha
stops()     : BelongsToMany     // Paradas desta linha
favoritedBy() : BelongsToMany   // Usuários que favoritaram
```

### Bus

```php
// Atributos (sincronizado em tempo real)
id          : integer (PK)
line_id     : integer (FK)
latitude    : decimal
longitude   : decimal
order       : integer          // Ordem na linha
vehicle     : string (número do veículo)
timestamp   : timestamp
created_at  : timestamp
updated_at  : timestamp

// Relacionamentos
line()      : BelongsTo     // Linha do ônibus
```

### BusStop

```php
// Atributos
id              : integer (PK)
sptrans_id      : integer (unique)
name            : string
latitude        : decimal
longitude       : decimal
created_at      : timestamp
updated_at      : timestamp

// Relacionamentos
lines()     : BelongsToMany    // Linhas que passam nesta parada
```

### Favorite

```php
// Atributos
id          : integer (PK)
user_id     : integer (FK)
line_id     : integer (FK)
created_at  : timestamp

// Relacionamentos
user()      : BelongsTo     // Usuário que favoritou
line()      : BelongsTo     // Linha favoritada
```

### Diagrama ER

```
┌─────────────┐
│    User     │
├─────────────┤
│ id (PK)     │
│ name        │
│ email       │
│ password    │
│ google_id   │
│ avatar      │
└─────┬───────┘
      │
      │ 1:N
      │
      ├──────────────┐
      │              │
      ▼              ▼
┌──────────────┐  ┌──────────────┐
│  Favorite    │  │ ApiToken     │
├──────────────┤  └──────────────┘
│ id (PK)      │
│ user_id (FK) │──┐
│ line_id (FK) │  │
└──────┬───────┘  │
       │          │
       ▼          │
    ┌──────────────┘
    │
    ▼
┌─────────────────┐         ┌──────────────┐
│     Line        │1:N      │    Bus       │
├─────────────────┤◄────────┤──────────────┤
│ id (PK)         │         │ id (PK)      │
│ sptrans_id      │         │ line_id (FK) │
│ direction_1     │         │ latitude     │
│ direction_2     │         │ longitude    │
│ color           │         └──────────────┘
│ lat_1, lng_1    │
│ lat_2, lng_2    │
└────────┬────────┘
         │
         │ M:N
         │
         ▼
    ┌──────────────┐
    │   BusStop    │
    ├──────────────┤
    │ id (PK)      │
    │ sptrans_id   │
    │ name         │
    │ latitude     │
    │ longitude    │
    └──────────────┘
```

---

## 🔧 Serviços e Lógica de Negócio

### AuthService

Responsável por autenticação e autorização.

```php
class AuthService
{
    // Login com credenciais
    public function login(string $email, string $password): array
    
    // Registrar novo usuário
    public function register(array $data): User
    
    // Autenticar com Google
    public function authenticateGoogle(string $code): array
    
    // Logout
    public function logout(User $user): bool
}
```

### LineService

Responsável pela lógica de linhas de ônibus.

```php
class LineService
{
    // Buscar linhas com filtros
    public function search(array $filters): LengthAwarePaginator
    
    // Detalhes de uma linha com ônibus em tempo real
    public function findWithBuses(int $id): ?array
    
    // Linhas próximas ao usuário
    public function nearby(float $lat, float $lng): Collection
    
    // Paradas de uma linha
    public function getStops(int $lineId): Collection
    
    // Posicionamento em tempo real dos ônibus
    private function getBusPositions(int $lineId): array
}
```

### BusStopService

Responsável pela lógica de paradas.

```php
class BusStopService
{
    // Paradas próximas
    public function nearest(float $lat, float $lng, float $radiusKm = 1): Collection
    
    // Paradas próximas com as linhas de cada uma
    public function nearestWithLines(float $lat, float $lng): Collection
    
    // Paradas próximas partindo de um endereço
    public function tripNearest(float $lat, float $lng): Collection
}
```

### ExternalApiService

Integração com APIs externas (SPTrans OlhoVivo).

```php
class ExternalApiService
{
    // Busca ônibus em tempo real na linha
    public function getBusPositions(int $lineSptransId): array
    
    // Busca paradas em tempo real
    public function getStops(int $lineSptransId): array
    
    // Previsão de chegada
    public function getArrivalPrediction(int $stopSptransId, int $lineSptransId): array
}
```

### ProcessorService

Integração com o Go Processor.

```php
class ProcessorService
{
    // Geocodifica um endereço
    public function geocode(string $address): ?GeoLocation
    
    // Reverse geocoding
    public function reverseGeocode(float $lat, float $lng): ?string
    
    // Calcula distância entre dois pontos
    public function distance(float $lat1, float $lng1, float $lat2, float $lng2): float
}
```

---

## 📡 Endpoints Principais

### Autenticação

#### Login

```
POST /api/auth/login
Content-Type: application/json

{
    "email": "user@example.com",
    "password": "password123"
}

Response 200:
{
    "token": "1|abc123xyz...",
    "user": {
        "id": 1,
        "name": "João",
        "email": "user@example.com",
        "avatar": null
    }
}

Response 401:
{
    "message": "The provided credentials are invalid."
}
```

#### Registrar

```
POST /api/auth/register
Content-Type: application/json

{
    "name": "João Silva",
    "email": "joao@example.com",
    "password": "password123",
    "password_confirmation": "password123"
}

Response 201:
{
    "token": "1|abc123xyz...",
    "user": {
        "id": 2,
        "name": "João Silva",
        "email": "joao@example.com",
        "avatar": null
    }
}

Response 422:
{
    "message": "The given data was invalid.",
    "errors": {
        "email": ["The email has already been taken."]
    }
}
```

#### Logout

```
POST /api/auth/logout
Authorization: Bearer 1|abc123xyz...

Response 200:
{
    "message": "Logged out successfully"
}
```

### Linhas

#### Listar todas as linhas

```
GET /api/lines?per_page=15&page=1
Authorization: Bearer token

Response 200:
{
    "data": [
        {
            "id": 1,
            "sptrans_id": 33624,
            "direction_1": "Centro",
            "direction_2": "Zona Sul",
            "main_stop_1": { "id": 1, "name": "Av. Paulista" },
            "main_stop_2": { "id": 2, "name": "Estação 25 de Março" },
            "color": "#FF0000",
            "route": "100-10"
        },
        ...
    ],
    "links": { "first": "...", "last": "..." },
    "meta": { "current_page": 1, "total": 500 }
}
```

#### Buscar linhas

```
GET /api/lines/search?term=100&per_page=15
Authorization: Bearer token

Response 200:
{
    "data": [
        {
            "id": 1,
            "sptrans_id": 33624,
            "direction_1": "Centro",
            "direction_2": "Zona Sul",
            ...
        },
        ...
    ],
    "meta": { "total": 5 }
}
```

#### Detalhes da linha com ônibus em tempo real

```
GET /api/lines/1
Authorization: Bearer token

Response 200:
{
    "line": {
        "id": 1,
        "sptrans_id": 33624,
        "direction_1": "Centro",
        "direction_2": "Zona Sul",
        "main_stop_1": { "id": 1, "name": "..." },
        "main_stop_2": { "id": 2, "name": "..." },
        "color": "#FF0000"
    },
    "buses": [
        {
            "id": 101,
            "vehicle": "70001",
            "latitude": -23.5505,
            "longitude": -46.6333,
            "order": 1,
            "updated_at": "2024-01-15T10:30:00Z"
        },
        ...
    ]
}

Response 404:
{
    "message": "Linha não encontrada"
}
```

#### Linhas próximas (geolocalização)

```
GET /api/lines/nearby?lat=-23.5505&lng=-46.6333&radius=5
Authorization: Bearer token

Response 200:
{
    "data": [
        {
            "id": 1,
            "sptrans_id": 33624,
            "direction_1": "Centro",
            "distance_km": 0.5,
            ...
        },
        ...
    ]
}
```

#### Paradas de uma linha

```
GET /api/lines/1/stops
Authorization: Bearer token

Response 200:
{
    "data": [
        {
            "id": 1,
            "sptrans_id": 1001,
            "name": "Av. Paulista, 1000",
            "latitude": -23.5505,
            "longitude": -46.6333,
            "distance_from_start_m": 0
        },
        ...
    ]
}
```

### Ônibus

#### Posição dos ônibus de uma linha

```
GET /api/lines/1/buses
Authorization: Bearer token

Response 200:
{
    "data": [
        {
            "id": 101,
            "vehicle": "70001",
            "latitude": -23.5505,
            "longitude": -46.6333,
            "order": 1,
            "updated_at": "2024-01-15T10:30:00Z"
        },
        ...
    ]
}
```

### Paradas

#### Paradas próximas

```
GET /api/stops/nearest?lat=-23.5505&lng=-46.6333&radius=1
Authorization: Bearer token

Response 200:
{
    "data": [
        {
            "id": 1,
            "sptrans_id": 1001,
            "name": "Av. Paulista, 1000",
            "latitude": -23.5505,
            "longitude": -46.6333,
            "distance_m": 50,
            "lines": [
                { "id": 1, "sptrans_id": 33624, "direction_1": "Centro" },
                ...
            ]
        },
        ...
    ]
}
```

#### Paradas próximas com trip (partida e destino)

```
GET /api/stops/trip-nearest?lat1=-23.5505&lng1=-46.6333&lat2=-23.5512&lng2=-46.6340
Authorization: Bearer token

Response 200:
{
    "origin_stops": [...],
    "destination_stops": [...]
}
```

### Geocodificação

#### Geocodificar endereço

```
POST /api/geocode
Content-Type: application/json

{
    "address": "Av. Paulista, São Paulo"
}

Response 200:
{
    "latitude": -23.5505,
    "longitude": -46.6333,
    "formatted_address": "Av. Paulista, São Paulo, Brasil"
}

Response 400:
{
    "message": "Endereço não encontrado"
}
```

#### Reverse geocoding

```
GET /api/geocode/reverse?lat=-23.5505&lng=-46.6333
Authorization: Bearer token

Response 200:
{
    "address": "Av. Paulista, 1000, São Paulo"
}
```

### Favoritos

#### Listar favoritos

```
GET /api/favorites
Authorization: Bearer token

Response 200:
{
    "data": [
        {
            "id": 1,
            "line_id": 1,
            "line": {
                "id": 1,
                "sptrans_id": 33624,
                "direction_1": "Centro",
                ...
            },
            "created_at": "2024-01-15T10:30:00Z"
        },
        ...
    ]
}
```

#### Adicionar favorito

```
POST /api/favorites
Authorization: Bearer token
Content-Type: application/json

{
    "line_id": 1
}

Response 201:
{
    "id": 1,
    "user_id": 1,
    "line_id": 1,
    "created_at": "2024-01-15T10:30:00Z"
}

Response 409:
{
    "message": "Linha já está nos favoritos"
}
```

#### Remover favorito

```
DELETE /api/favorites/1
Authorization: Bearer token

Response 204:
```

---

## 🚨 Tratamento de Erros

### Códigos HTTP

| Código | Significado | Exemplo |
| -------- | ----------- | --------- |
| 200 | OK - Requisição bem-sucedida | GET /api/lines |
| 201 | Created - Recurso criado | POST /api/favorites |
| 204 | No Content - Sucesso sem corpo | DELETE /api/favorites/1 |
| 400 | Bad Request - Requisição inválida | JSON malformado |
| 401 | Unauthorized - Token inválido/expirado | Token expirou |
| 403 | Forbidden - Sem permissão | Outro usuário |
| 404 | Not Found - Recurso não existe | GET /api/lines/999 |
| 409 | Conflict - Conflito | Favorito duplicado |
| 422 | Unprocessable Entity - Validação falhou | Email inválido |
| 429 | Too Many Requests - Rate limit | Muitas requisições |
| 500 | Internal Server Error | Erro no servidor |

### Formato de Erro

```json
{
    "message": "Descrição do erro",
    "errors": {
        "field_name": ["Mensagem de validação"],
        "another_field": ["Outro erro"]
    }
}
```

### Exemplos

#### Validação falha

```json
{
    "message": "The given data was invalid.",
    "errors": {
        "email": ["The email field is required."],
        "password": ["The password must be at least 8 characters."]
    }
}
```

#### Recurso não encontrado

```json
{
    "message": "Linha não encontrada"
}
```

#### Não autenticado

```json
{
    "message": "Unauthenticated."
}
```

---

## ✅ Testes

### Estrutura

```
tests/
├── Feature/
│   ├── AuthTest.php          # Testes de autenticação
│   ├── LineTest.php          # Testes de linhas
│   ├── BusTest.php           # Testes de ônibus
│   ├── BusStopTest.php       # Testes de paradas
│   └── FavoriteTest.php      # Testes de favoritos
│
└── Unit/
    ├── AuthServiceTest.php
    ├── LineServiceTest.php
    └── ...
```

### Executar Testes

```bash
# Todos os testes
php artisan test

# Apenas Feature tests
php artisan test --filter Feature

# Apenas Unit tests
php artisan test --filter Unit

# Teste específico
php artisan test tests/Feature/AuthTest.php

# Com coverage
php artisan test --coverage

# Para na primeira falha
php artisan test --stop-on-failure
```

### Exemplo de Teste

```php
// tests/Feature/LineTest.php
use Tests\TestCase;

class LineTest extends TestCase
{
    public function test_can_list_lines()
    {
        $user = User::factory()->create();
        $lines = Line::factory(5)->create();

        $response = $this->actingAs($user)
            ->getJson('/api/lines');

        $response->assertStatus(200)
                 ->assertJsonCount(5, 'data');
    }

    public function test_can_search_lines()
    {
        $user = User::factory()->create();
        Line::factory()->create(['direction_1' => 'Centro']);
        Line::factory()->create(['direction_1' => 'Zona Sul']);

        $response = $this->actingAs($user)
            ->getJson('/api/lines/search?term=Centro');

        $response->assertStatus(200)
                 ->assertJsonCount(1, 'data');
    }
}
```

---

## 🎨 Decisões de Design

### 1. **Arquitetura em Camadas (Service Layer)**

**Decisão:** Separar Controllers → Services → Repositories

**Benefícios:**

- Lógica de negócio centralizada
- Fácil de testar em isolamento
- Reutilização entre endpoints
- Mudanças isoladas por camada

**Trade-off:** Mais arquivos, mais abstração inicial

### 2. **DTOs para Response**

**Decisão:** Usar Data Transfer Objects em vez de serializar Models diretamente

**Benefícios:**

- Controle fino sobre dados expostos
- Validação explícita
- Transformação de dados
- Não expõe relacionamentos sensíveis

### 3. **Sanctum para Autenticação**

**Decisão:** Token stateless ao invés de sessões

**Benefícios:**

- Funciona com SPA e apps mobile
- Escalável horizontalmente
- Multi-device support
- Segurança contra CSRF

### 4. **MySQL como BD Principal**

**Decisão:** Não usar SQLite em produção

**Benefícios:**

- Melhor performance em escala
- Suporte a transações complexas
- Replicação para alta disponibilidade
- Backup e recovery

### 5. **Cache em BD por Padrão**

**Decisão:** Usar banco de dados para cache/session/queue

**Benefícios:**

- Simples de setup
- Sem dependências externas
- Funciona em qualquer ambiente
- Persiste entre restarts

**Upgrade para Production:**

- Cache → Redis
- Queue → Redis
- Session → Redis

---

## 📚 Arquivos de Configuração

### composer.json

Define dependências PHP e scripts de instalação.

### phpunit.xml

Configuração do PHPUnit para testes.

### vite.config.js

Configuração do Vite para build de assets.

### tailwind.config.js

Configuração do Tailwind CSS.

### .env.example

Exemplo de arquivo .env com todas as variáveis necessárias.

---

## 🔗 Relacionados

- [README.md](../README.md) - Visão geral do projeto
- [PROCESSOR.md](../PROCESSOR.md) - Documentação do Processor
- [Documentação Laravel](https://laravel.com/docs)
- [Documentação Sanctum](https://laravel.com/docs/sanctum)
- [Documentação Socialite](https://laravel.com/docs/socialite)

---

## 📞 Suporte

Para dúvidas específicas do backend:

1. Consulte este documento
2. Procure no código os comentários
3. Verifique os testes para exemplos de uso
4. Abra uma issue no repositório
