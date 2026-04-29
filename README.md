# 🚍 Urban Mobility API

> API RESTful para mobilidade urbana em São Paulo — dados em tempo real via **SPTrans OlhoVivo**, autenticação segura e arquitetura profissional em camadas.

---

## 📋 Índice

- [Visão Geral](#-visão-geral)
- [Stack](#-stack)
- [Arquitetura](#-arquitetura)
- [Fluxo de Autenticação](#-fluxo-de-autenticação)
- [Integração SPTrans](#-integração-sptrans)
- [Banco de Dados](#-banco-de-dados)
- [Endpoints](#-endpoints)
- [Setup](#-setup)
- [Variáveis de Ambiente](#-variáveis-de-ambiente)
- [Decisões de Design](#-decisões-de-design)

---

## 🎯 Visão Geral

O **Urban Mobility API** é um backend que centraliza informações de transporte público de São Paulo, oferecendo:

- 🔐 Autenticação via token (Sanctum) e OAuth Google (Socialite)
- 🚌 Posicionamento em tempo real dos ônibus via SPTrans OlhoVivo
- ⭐ Sistema de linhas favoritas por usuário
- 🔍 Busca de linhas por nome ou número
- 📍 Previsão de chegada por parada

---

## 🚀 Stack

| Camada | Tecnologia |
|--------|-----------|
| Linguagem | PHP 8.3+ |
| Framework | Laravel 11 |
| Autenticação | Laravel Sanctum + Socialite |
| Banco (dev) | SQLite |
| Banco (prod) | MySQL 8 |
| Cache | Redis / File Cache |
| HTTP Client | Guzzle (via Laravel Http) |
| API Externa | SPTrans OlhoVivo v2.1 |

---

## 🧠 Arquitetura

O projeto segue arquitetura em camadas com separação clara de responsabilidades:

```
┌─────────────────────────────────────────────────────────┐
│                        CLIENT                           │
│              (Postman / App Mobile / SPA)               │
└────────────────────────┬────────────────────────────────┘
                         │ HTTP Request
                         ▼
┌─────────────────────────────────────────────────────────┐
│                   ROUTES (api.php)                      │
│         auth:sanctum middleware | prefix /api           │
└────────────────────────┬────────────────────────────────┘
                         │
                         ▼
┌─────────────────────────────────────────────────────────┐
│               FORM REQUESTS (Validation)                │
│     LoginRequest / RegisterRequest / SearchLineRequest  │
└────────────────────────┬────────────────────────────────┘
                         │ validated()
                         ▼
┌─────────────────────────────────────────────────────────┐
│                    CONTROLLERS                          │
│        AuthController / LineController /                │
│        BusController / FavoriteController               │
└────────────────────────┬────────────────────────────────┘
                         │ delegates to
                         ▼
┌─────────────────────────────────────────────────────────┐
│                     SERVICES                            │
│          AuthService / LineService                      │
│              ┌──────────────────┐                       │
│              │ ExternalApiService│ ◄── SPTrans OlhoVivo │
│              └──────────────────┘                       │
└──────────┬─────────────────────────────────────────────┘
           │ queries via
           ▼
┌─────────────────────────────────────────────────────────┐
│                   REPOSITORIES                          │
│           LineRepository / FavoriteRepository           │
└────────────────────────┬────────────────────────────────┘
                         │ Eloquent ORM
                         ▼
┌─────────────────────────────────────────────────────────┐
│                      MODELS                             │
│           User / Line / Bus / Favorite                  │
└────────────────────────┬────────────────────────────────┘
                         │
                         ▼
┌─────────────────────────────────────────────────────────┐
│                     DATABASE                            │
│                  SQLite / MySQL                         │
└─────────────────────────────────────────────────────────┘
```

### Fluxo de dados — busca de linha com ônibus em tempo real

```
GET /api/lines/{id}
        │
        ▼
LineController::show(id)
        │
        ▼
LineService::findWithBuses(id)
        │
        ├──► LineRepository::findById(id) ──► Database ──► Line model
        │
        └──► ExternalApiService::getBusPositionsByLine(sptrans_id)
                    │
                    ├── Cache HIT?  ──► retorna posições cacheadas (TTL 30s)
                    │
                    └── Cache MISS? ──► POST /Login/Autenticar (SPTrans)
                                            │
                                            └──► GET /Posicao/Linha?codigoLinha=X
                                                        │
                                                        ▼
                                               mapBusPositions()
                                                        │
                                                        ▼
                                                   BusDTO[]
                                                        │
                                                        ▼
                                              Cache::put (30s)
        │
        ▼
LineDTO + BusDTO[]
        │
        ▼
JsonResponse { line, buses }
```

---

## 🔐 Fluxo de Autenticação

### Sanctum (Email/Senha)

```
POST /api/auth/register
POST /api/auth/login
        │
        ▼
AuthService::login() / register()
        │
        ▼
User::createToken('api-token')
        │
        ▼
{ user, token: "1|abc123..." }
        │
        ▼
Authorization: Bearer 1|abc123...  ← header em todas as rotas protegidas
```

### Google OAuth (Socialite)

```
GET /api/auth/google/redirect
        │
        ▼
Google OAuth Consent Screen
        │
        ▼
GET /api/auth/google/callback
        │
        ▼
Socialite::driver('google')->user()
        │
        ▼
User::updateOrCreate(['google_id' => ...])
        │
        ▼
{ user, token }
```

---

## 🔌 Integração SPTrans

A classe `ExternalApiService` implementa o protocolo completo da [OlhoVivo API v2.1](https://www.sptrans.com.br/desenvolvedores/):

```
┌─────────────────────────────────────────────────────────┐
│                  ExternalApiService                     │
│                                                         │
│  authenticate()                                         │
│    POST https://api.olhovivo.sptrans.com.br/v2.1/       │
│         Login/Autenticar?token={TOKEN}                  │
│    → Seta cookie apiCredentials                         │
│    → Persiste cookie no Redis (TTL 1h)                  │
│                                                         │
│  getBusPositionsByLine(sptransId)                       │
│    GET /Posicao/Linha?codigoLinha={id}                  │
│    → Cache Redis 30s                                    │
│    → Retorna BusDTO[]                                   │
│                                                         │
│  searchLines(term)                                      │
│    GET /Linha/Buscar?termosBusca={term}                 │
│    → Cache Redis 10min                                  │
│                                                         │
│  getArrivalPrediction(codigoParada, codigoLinha)        │
│    GET /Previsao?codigoParada=X&codigoLinha=Y           │
│    → Sem cache (dados em tempo real)                    │
│                                                         │
│  getStopsByLine(codigoLinha)                            │
│    GET /Parada/BuscarParadasPorLinha?codigoLinha={id}   │
│    → Cache Redis 30min                                  │
└─────────────────────────────────────────────────────────┘
```

### Estratégia de Cache

| Dado | TTL | Justificativa |
|------|-----|---------------|
| Cookie de sessão | 1 hora | Sessão SPTrans é longa |
| Posição dos ônibus | 30 segundos | Muda constantemente |
| Lista de linhas | 10 minutos | Muda raramente |
| Paradas por linha | 30 minutos | Dados estáticos |

---

## 🗄️ Banco de Dados

### Diagrama de Entidades

```
┌─────────────────┐          ┌─────────────────┐
│     users       │          │      lines       │
├─────────────────┤          ├─────────────────┤
│ id (PK)         │          │ id (PK)          │
│ name            │          │ code            │
│ email (unique)  │          │ name            │
│ password        │          │ origin          │
│ google_id       │          │ destination     │
│ created_at      │          │ circular (bool) │
│ updated_at      │          │ sptrans_id      │
└────────┬────────┘          │ created_at      │
         │                   │ updated_at      │
         │                   └────────┬────────┘
         │                            │
         │    ┌──────────────────┐    │
         │    │    favorites     │    │
         │    ├──────────────────┤    │
         └───►│ user_id (FK)     │◄───┘
              │ line_id (FK)     │
              │ created_at       │
              │                  │
              │ UNIQUE(user_id,  │
              │        line_id)  │
              └──────────────────┘
```

---

## 📡 Endpoints

### Autenticação (público)

| Método | Rota | Body | Descrição |
|--------|------|------|-----------|
| `POST` | `/api/auth/register` | `name, email, password, password_confirmation` | Cadastro |
| `POST` | `/api/auth/login` | `email, password` | Login |
| `GET` | `/api/auth/google/redirect` | — | Inicia OAuth Google |
| `GET` | `/api/auth/google/callback` | — | Callback Google |

### Linhas (🔒 auth:sanctum)

| Método | Rota | Query | Descrição |
|--------|------|-------|-----------|
| `GET` | `/api/lines` | `?term=8000&per_page=15` | Lista linhas (paginado) |
| `GET` | `/api/lines/{id}` | — | Detalhe da linha + ônibus em tempo real |
| `GET` | `/api/lines/{id}/buses` | — | Só posição dos ônibus |

### Favoritos (🔒 auth:sanctum)

| Método | Rota | Body | Descrição |
|--------|------|------|-----------|
| `GET` | `/api/favorites` | — | Lista favoritos do usuário |
| `POST` | `/api/favorites` | `line_id` | Adiciona favorito |
| `DELETE` | `/api/favorites/{id}` | — | Remove favorito |
| `POST` | `/api/auth/logout` | — | Logout (revoga token) |

### Exemplo de resposta — `GET /api/lines/{id}`

```json
{
  "message": "Linha recuperada com sucesso.",
  "data": {
    "line": {
      "id": 1,
      "code": "8000",
      "name": "PCA.RAMOS DE AZEVEDO - TERMINAL LAPA",
      "origin": "PCA.RAMOS DE AZEVEDO",
      "destination": "TERMINAL LAPA",
      "circular": false,
      "sptrans_id": 1273
    },
    "buses": [
      {
        "prefix": "11433",
        "accessible": false,
        "captured_at": "2026-04-29T19:57:02Z",
        "latitude": -23.540150,
        "longitude": -46.644140
      }
    ]
  }
}
```

---

## ⚙️ Setup

### Pré-requisitos

- PHP 8.3+
- Composer
- Redis (para cache)
- Token SPTrans OlhoVivo → [sptrans.com.br/desenvolvedores](https://www.sptrans.com.br/desenvolvedores/)
- Credenciais Google OAuth → [console.cloud.google.com](https://console.cloud.google.com/)

### Instalação

```bash
# 1. Clone o repositório
git https://github.com/hltav/mobility_project_backend
cd mobility_project_backend

# 2. Instale as dependências
composer install

# 3. Configure o ambiente
cp .env.example .env
php artisan key:generate

# 4. Configure o banco e rode as migrations
php artisan migrate

# 5. (Opcional) Sincronize as linhas da SPTrans
php artisan sptrans:sync

# 6. Inicie o servidor
php artisan serve
```

### Comandos úteis

```bash
# Sincroniza linhas com a SPTrans
php artisan sptrans:sync

# Limpa todos os caches
php artisan cache:clear && php artisan config:clear

# Roda os testes
php artisan test
```

---

## 🔧 Variáveis de Ambiente

```env
APP_NAME="Urban Mobility"
APP_ENV=local
APP_URL=http://localhost:8000

# Banco
DB_CONNECTION=sqlite      # dev
# DB_CONNECTION=mysql     # prod

# Cache — recomendado Redis em produção
CACHE_DRIVER=redis
REDIS_HOST=127.0.0.1
REDIS_PORT=6379

# SPTrans OlhoVivo
# Cadastre-se em: https://www.sptrans.com.br/desenvolvedores/
SPTRANS_API_TOKEN=seu_token_aqui

# Google OAuth (Socialite)
GOOGLE_CLIENT_ID=seu_client_id
GOOGLE_CLIENT_SECRET=seu_client_secret
GOOGLE_REDIRECT_URI=http://localhost:8000/api/auth/google/callback
```

---

## 💡 Decisões de Design

### Por que Repository Pattern?
Desacopla as queries do Service, facilitando testes unitários com mocks e eventual troca de ORM ou banco.

### Por que DTOs?
Garante que nenhum campo inesperado da SPTrans vaze para a resposta da API. O `LineDTO::fromSptrans()` e `BusDTO::fromArray()` são os únicos pontos de transformação — fácil de auditar e manter.

### Por que cache em camadas?
A SPTrans tem rate limiting implícito. Cache de 30s na posição dos ônibus reduz chamadas externas em ~95% sem perder relevância dos dados.

### Por que Sanctum em vez de Passport?
O projeto serve SPA e mobile. Sanctum é leve, sem overhead de OAuth server completo, e o token Bearer funciona perfeitamente para ambos os casos.

### Por que `firstOrCreate` nos favoritos?
Evita race conditions em requisições simultâneas sem precisar de lock explícito — o banco garante a unicidade via constraint `UNIQUE(user_id, line_id)`.

---

## 📁 Estrutura de Pastas

```
app/
├── Http/
│   ├── Controllers/
│   │   ├── AuthController.php
│   │   ├── BusController.php
│   │   ├── LineController.php
│   │   └── FavoriteController.php
│   └── Requests/
│       ├── Auth/
│       │   ├── LoginRequest.php
│       │   └── RegisterRequest.php
│       ├── Line/
│       │   └── SearchLineRequest.php
│       └── Favorite/
│           └── StoreFavoriteRequest.php
├── Models/
│   ├── User.php
│   ├── Line.php
│   ├── Bus.php
│   └── Favorite.php
├── Services/
│   ├── AuthService.php
│   ├── LineService.php
│   └── ExternalApiService.php
├── Repositories/
│   ├── LineRepository.php
│   └── FavoriteRepository.php
└── DTOs/
    ├── LineDTO.php
    └── BusDTO.php
```

---

## 📄 Licença

MIT © Hudson Tavares
